package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/keybase/go-keybase-chat-bot/kbchat"
	"github.com/keybase/go-keybase-chat-bot/kbchat/types/chat1"
	"github.com/keybase/managed-bots/base"
	"github.com/keybase/managed-bots/webhookbot/webhookbot"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	*base.Options
	HTTPPrefix string
}

func NewOptions() *Options {
	return &Options{
		Options: base.NewOptions(),
	}
}

type BotServer struct {
	*base.Server

	opts Options
	kbc  *kbchat.API
}

func NewBotServer(opts Options) *BotServer {
	return &BotServer{
		Server: base.NewServer("webhookbot", opts.Announcement, opts.AWSOpts, opts.MultiDSN, opts.ReadSelf, kbchat.RunOptions{
			KeybaseLocation: opts.KeybaseLocation,
			HomeDir:         opts.Home,
		}),
		opts: opts,
	}
}

const back = "`"
const backs = "```"

func (s *BotServer) makeAdvertisement() kbchat.Advertisement {
	createExtended := fmt.Sprintf(`Create a new webhook for sending messages into the current conversation. You must supply a name as well to identify the webhook. To use a webhook URL, supply a %smsg%s URL parameter, or a JSON POST body with a field %smsg%s. You can also supply a template, which allows you to customize the message displayed by the webhook, and the URL and/or JSON fields it will accept. For more information on templates, use the %s!webhook help%s command.

	Example:%s
		!webhook create alerts%s

	Example (using custom template):%s
		!webhook create alerts *{{.title}}*
		%s{{.body}}%s%s`,
		back, back, back, back, back, back, backs, backs, backs, back, back, backs)
	updateExtended := fmt.Sprintf(`Update an existing webhook's template. Leave the template field empty to use the default template. For more information on templates, use the %s!webhook help%s command.

	Example:%s
		!webhook update alerts *New Alert: {{.title}}*
		%s{{.body}}%s%s`, back, back, backs, back, back, backs)
	removeExtended := fmt.Sprintf(`Remove a webhook from the current conversation. You must supply the name of the webhook.

	Example:%s
		!webhook remove alerts%s`, backs, backs)

	cmds := []chat1.UserBotCommandInput{
		{
			Name:        "webhook create",
			Usage:       "<name> [<template>]",
			Description: "Create a new webhook for sending into the current conversation",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title: `*!webhook create* <name> [<template>]
Create a webhook`,
				DesktopBody: createExtended,
				MobileBody:  createExtended,
			},
		},
		{
			Name:        "webhook update",
			Usage:       "<name> [<template>]",
			Description: "Update the template of an existing webhook in the current conversation",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title: `*!webhook update* <name> [<template>]
Update a webhook's template`,
				DesktopBody: updateExtended,
				MobileBody:  updateExtended,
			},
		},
		{
			Name:        "webhook list",
			Description: "List active webhooks in the current conversation",
		},
		{
			Name:        "webhook remove",
			Description: "Remove a webhook from the current conversation",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title: `*!webhook remove* <name>
Remove a webhook`,
				DesktopBody: removeExtended,
				MobileBody:  removeExtended,
			},
		},
		{
			Name:        "webhook help",
			Description: "Get more information about using templates",
		},
		base.GetFeedbackCommandAdvertisement(s.kbc.GetUsername()),
	}
	return kbchat.Advertisement{
		Alias: "Webhooks",
		Advertisements: []chat1.AdvertiseCommandAPIParam{
			{
				Typ:      "public",
				Commands: cmds,
			},
		},
	}
}

func (s *BotServer) Go() (err error) {
	if s.kbc, err = s.Start(s.opts.ErrReportConv); err != nil {
		return err
	}
	sdb, err := sql.Open("mysql", s.opts.DSN)
	if err != nil {
		s.Errorf("failed to connect to MySQL: %s", err)
		return err
	}
	defer sdb.Close()
	db := webhookbot.NewDB(sdb)

	debugConfig := base.NewChatDebugOutputConfig(s.kbc, s.opts.ErrReportConv)
	stats, err := base.NewStatsRegistry(debugConfig, s.opts.StathatEZKey)
	if err != nil {
		s.Debug("unable to create stats: %v", err)
		return err
	}
	stats = stats.SetPrefix(s.Name())
	httpSrv := webhookbot.NewHTTPSrv(stats, debugConfig, db)
	handler := webhookbot.NewHandler(stats, s.kbc, debugConfig, httpSrv, db, s.opts.HTTPPrefix)
	eg := &errgroup.Group{}
	s.GoWithRecover(eg, func() error { return s.Listen(handler) })
	s.GoWithRecover(eg, httpSrv.Listen)
	s.GoWithRecover(eg, func() error { return s.HandleSignals(httpSrv, stats) })
	s.GoWithRecover(eg, func() error { return s.AnnounceAndAdvertise(s.makeAdvertisement(), "I live.") })
	if err := eg.Wait(); err != nil {
		s.Debug("wait error: %s", err)
		return err
	}
	return nil
}

func main() {
	rc := mainInner()
	os.Exit(rc)
}

func mainInner() int {
	opts := NewOptions()
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&opts.HTTPPrefix, "http-prefix", os.Getenv("BOT_HTTP_PREFIX"),
		"Desired prefix for generated webhooks")
	if err := opts.Parse(fs, os.Args); err != nil {
		fmt.Printf("Unable to parse options: %v\n", err)
		return 3
	}
	if len(opts.DSN) == 0 {
		fmt.Printf("must specify a database DSN\n")
		return 3
	}
	bs := NewBotServer(*opts)
	if err := bs.Go(); err != nil {
		fmt.Printf("error running chat loop: %s\n", err)
		return 3
	}
	return 0
}
