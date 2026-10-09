package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

func teamConfig(a *app, b store.Bot) (connect.Config, error) { return team.Config(a.st, b, a.fallback) }

func (a *app) memoryDetails(c *ui.Context, b store.Bot) {}
