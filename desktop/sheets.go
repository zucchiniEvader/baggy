package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/provider/connect"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

func errf(format string, a ...any) error { return fmt.Errorf(L(format), a...) }

// ---- bots ----

type botForm struct {
	open                bool
	id                  string // "" for a new bot
	name, description   string
	providerName, model string
	thinking            string
	icon                string // a botIcons name
	color               string // a botColors name; "" until one is picked
	avatar              string // its picture (store.Bot.Avatar); "" for the icon
	stored              string // the picture it had, kept until it saves
	workdir             string // where its tools run; "" is its workspace
	err                 string
}

var thinkingLevels = []string{"Default", "Off", "Low", "Medium", "High"}

func (a *app) providerNames() []string {
	names := []string{L("Default")}
	ps, _ := a.st.Providers()
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names
}

func (a *app) providerID(name string) string {
	ps, _ := a.st.Providers()
	for _, p := range ps {
		if p.Name == name {
			return p.ID
		}
	}
	return ""
}

func (a *app) providerName(id string) string {
	if p, err := a.st.Provider(id); err == nil {
		return p.Name
	}
	return L("Default")
}

func (a *app) newBot() {
	a.botSheet = &botForm{open: true, providerName: L("Default"), thinking: "Default", icon: "bot", color: colorName(store.Bot{ID: store.NewID("bot")})}
}

func (a *app) editBot(id string) {
	b, ok := a.byID[id]
	if !ok {
		return
	}
	th := "Default"
	for _, l := range thinkingLevels {
		if strings.EqualFold(l, b.Thinking) {
			th = l
		}
	}
	a.botSheet = &botForm{open: true, id: b.ID, name: b.Name, description: b.Description, providerName: a.providerName(b.Provider), model: b.Model, thinking: th,
		icon: b.Icon, color: colorName(b), avatar: b.Avatar, stored: b.Avatar, workdir: b.Workdir}
}

func (a *app) saveBot(f *botForm) bool {
	th := strings.ToLower(f.thinking)
	if th == "default" {
		th = ""
	}
	b := store.Bot{ID: f.id, Name: f.name, Description: f.description, Provider: a.providerID(f.providerName), Model: strings.TrimSpace(f.model), Thinking: th,
		Icon: f.icon, Color: f.color, Avatar: f.avatar, Workdir: strings.TrimSpace(f.workdir)}
	var err error
	if f.id == "" {
		b, err = a.st.CreateBot(b)
		if err == nil {
			a.reload()
			if dm, derr := a.st.DMOf(b.ID); derr == nil {
				a.selectChat(dm.ID)
			}
		}
	} else {
		err = a.st.UpdateBot(b)
		a.reload()
	}
	if err != nil {
		f.err = err.Error()
		return false
	}
	f.stored = f.avatar
	return true
}

// dropPicture forgets a picture chosen in the form and not saved.
func (a *app) dropPicture(f *botForm) {
	if f.avatar != "" && f.avatar != f.stored {
		os.Remove(a.st.AvatarPath(f.avatar))
	}
	f.avatar = f.stored
}

func (a *app) botSheetView(c *ui.Context) {
	f := a.botSheet
	ui.Modal(c, &f.open, func() {
		title := L("New Bot")
		action := L("Create")
		if f.id != "" {
			title, action = L("Edit %s", f.name), L("Save")
		}
		dialogBody(c, 480, 18).Children(func() {
			dialogTitle(c, title, L("A bot is a name, a description (its system prompt), the model it runs on, and a workspace folder of its own."))
			a.avatarEditor(c, f)
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { textField(c, &f.name, L("For example Scout")).AutoFocus() })
				ui.Field(c, L("Description"), func() { textArea(c, &f.description, L("What it does and how: its system prompt."), 4, 10) })
				ui.Field(c, L("Provider"), func() { popup(c, &f.providerName, a.providerNames()) })
				ui.Field(c, L("Model"), func() { textField(c, &f.model, a.modelHint(f.providerName)) })
				ui.Field(c, L("Thinking"), func() { popup(c, &f.thinking, thinkingLevels) })
				ui.Field(c, L("Folder"), func() { a.folderField(c, f) })
			})
			if f.err != "" {
				ui.Text(c, f.err).FontSize(12).TextColor(pal(c).red)
			}
			if dialogButtons(c, &f.open, action, strings.TrimSpace(f.name) == "") && a.saveBot(f) {
				f.open = false
			}
		})
	})
	if !f.open {
		a.dropPicture(f)
	}
}

// avatarEditor is the bot's avatar as it will look, beside what makes it:
// its own picture, or an icon on a color.
func (a *app) avatarEditor(c *ui.Context, f *botForm) {
	t := c.Theme()
	l := look{icon: botIcon(f.icon), color: colorOf(f.color)}
	if f.avatar != "" {
		l.picture = a.st.AvatarPath(f.avatar)
	}
	ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
		a.avatarOf(c, l, 64).Label(L("Avatar"))
		ui.Column(c).Grow(1).MinWidth(0).Gap(10).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if a.pickImage != nil && smallButton(c.Key("pick-picture"), L("Choose Picture…"), secondary).Clicked() {
					a.pickImage(func(path string) {
						if err := a.setPicture(f, path); err != nil {
							f.err = err.Error()
						} else if f.avatar != f.stored {
							f.err = ""
						}
					})
				}
				if f.avatar != "" && smallButton(c.Key("drop-picture"), L("Use Icon"), ghost).Clicked() {
					if f.avatar != f.stored {
						os.Remove(a.st.AvatarPath(f.avatar))
					}
					f.avatar = ""
				}
			})
			if f.avatar != "" {
				ui.Text(c, L("The picture shows in place of the icon.")).FontSize(11).TextColor(t.TextMuted)
				return
			}
			a.iconPicker(c, &f.icon)
			a.colorPicker(c, &f.color)
		})
	})
}

// folderField is where a bot's tools run: a folder the user chose, or its
// own workspace when empty.
func (a *app) folderField(c *ui.Context, f *botForm) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1).MinWidth(0).Children(func() { textField(c.Key("workdir"), &f.workdir, L("Its own workspace")) })
			if a.pickFolder != nil && smallButton(c.Key("pick-folder"), L("Choose…"), secondary).Clicked() {
				a.pickFolder(func(path string) { f.workdir = path })
			}
		})
		ui.Text(c, L("Where its tools run, as a project folder. Its memory stays in its own workspace.")).FontSize(11).TextColor(c.Theme().TextMuted)
	})
}

// colorPicker chooses an avatar's color, as a row of dots.
func (a *app) colorPicker(c *ui.Context, name *string) {
	ui.Row(c).Gap(6).Wrap().FocusGroup(ui.Horizontal).Children(func() {
		for _, bc := range botColors {
			chosen := *name == bc.name
			dot := ui.ButtonBase(c.Key("color-"+bc.name)).Size(20, 20).Radius(10).Center().Background(bc.color).Label(L(bc.name)).Tooltip(L(bc.name)).Role(ui.RoleRadio).Checked(chosen)
			if chosen { // a ring of the dialog inside one of its own color
				dot.Border(2, pal(c).surface).Shadow(0, 0, 0, 1.5, bc.color)
			}
			if dot.Clicked() {
				*name = bc.name
			}
		}
	})
}

// iconPicker chooses a bot's icon, as a row of tiles.
func (a *app) iconPicker(c *ui.Context, name *string) {
	t := c.Theme()
	ui.Row(c).Gap(4).Wrap().FocusGroup(ui.Horizontal).Children(func() {
		for _, b := range botIcons {
			chosen := *name == b.name || (*name == "" && b.name == "bot")
			tile := ui.ButtonBase(c.Key("look-"+b.name)).Size(compactHeight, compactHeight).Radius(rowRadius).Center().Label(b.name).Role(ui.RoleRadio).Checked(chosen)
			st := rowPlain
			if chosen {
				st = rowChosen
			}
			styleRow(tile, t, st)
			if tile.Clicked() {
				*name = b.name
			}
			tile.Children(func() {
				ic := ui.Icon(c, b.svg).FontSize(iconSize)
				if !chosen {
					ic.TextColor(t.TextMuted)
				}
			})
		}
	})
}

// modelHint is the model a bot runs on with no model set.
func (a *app) modelHint(providerName string) string {
	b := store.Bot{Provider: a.providerID(providerName)}
	if cfg, err := team.Config(a.st, b, a.fallback); err == nil {
		return cfg.Model
	}
	return L("model id")
}

// ---- groups ----

type groupForm struct {
	open        bool
	id          string
	title, desc string
	picked      map[string]bool
	err         string
}

func (a *app) newGroup() {
	a.groupSheet = &groupForm{open: true, picked: map[string]bool{}}
}

func (a *app) editGroup(ch store.Chat) {
	a.groupSheet = &groupForm{open: true, id: ch.ID, title: ch.Title, desc: ch.Description}
}

func (a *app) saveGroup(f *groupForm) bool {
	if f.id != "" {
		if err := a.st.SetGroupInfo(f.id, f.title, f.desc); err != nil {
			f.err = err.Error()
			return false
		}
		a.reload()
		return true
	}
	var members []string
	for _, b := range a.bots {
		if f.picked[b.ID] {
			members = append(members, b.ID)
		}
	}
	g, err := a.st.CreateGroup(f.title, f.desc, members)
	if err != nil {
		f.err = err.Error()
		return false
	}
	a.reload()
	a.selectChat(g.ID)
	return true
}

func (a *app) groupSheetView(c *ui.Context) {
	t := c.Theme()
	f := a.groupSheet
	ui.Modal(c, &f.open, func() {
		title, action := L("New Group"), L("Create")
		if f.id != "" {
			title, action = L("Edit Group"), L("Save")
		}
		dialogBody(c, 480, 18).Children(func() {
			dialogTitle(c, title, L("Members take turns: the ones you @ go first, and each replies or passes."))
			ui.Form(c, func() {
				ui.Field(c, L("Name"), func() { textField(c, &f.title, L("For example Launch")).AutoFocus() })
				ui.Field(c, L("Description"), func() { textArea(c, &f.desc, L("What the group is for; every member reads it."), 2, 6) })
			})
			if f.id == "" {
				ui.Column(c).Gap(1).FocusGroup(ui.Vertical).Children(func() {
					ui.Text(c, L("Members (the first one owns the work)")).FontSize(13).FontWeight(600).Padding(0, 0, 5)
					for _, b := range a.bots {
						on := f.picked[b.ID]
						row := listRow(c.Key("pick-"+b.ID), rowPlain).Label(b.Name).Role(ui.RoleCheckBox).Checked(on)
						if row.Clicked() {
							f.picked[b.ID] = !on
						}
						row.Children(func() {
							a.avatar(c, b.ID, 20)
							ui.Text(c, b.Name).FontSize(13).Shrink(0)
							ui.Text(c, firstLine(b.Description)).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
							ui.Spacer(c)
							if on {
								ui.Icon(c, iconCheck).FontSize(iconSize).Shrink(0)
							}
						})
					}
				})
			}
			if f.err != "" {
				ui.Text(c, f.err).FontSize(12).TextColor(pal(c).red)
			}
			if dialogButtons(c, &f.open, action, strings.TrimSpace(f.title) == "") && a.saveGroup(f) {
				f.open = false
			}
		})
	})
}

// ---- providers ----

type providerForm struct {
	showKey                bool // the key shows as text
	checking, checkOK      bool // Test Connection under way, and how it went
	check                  string
	id                     string
	name, api, base, key   string
	models                 string // comma separated
	replay, encrypted, web bool
	err                    string

	// What the server lists (connect.ListModels), to check models from.
	listing    bool
	listed     []connect.Listed
	listErr    string
	listQuery  string
	listedFor  string    // the protocol, URL and key the list is for
	pendingFor string    // and those being typed, listed once they rest
	editedAt   time.Time // when pendingFor last changed
}

var apiLabels = []string{"Anthropic Messages", "OpenAI Chat Completions", "OpenAI Responses"}
var apiIDs = []string{connect.Anthropic, connect.Chat, connect.Responses}

func apiLabel(id string) string {
	for i, x := range apiIDs {
		if x == id {
			return apiLabels[i]
		}
	}
	return apiLabels[0]
}

func apiID(label string) string {
	for i, x := range apiLabels {
		if x == label {
			return apiIDs[i]
		}
	}
	return connect.Anthropic
}

func (a *app) saveProvider(f *providerForm) bool {
	var models []string
	for _, m := range strings.Split(f.models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	if strings.TrimSpace(f.name) == "" || len(models) == 0 {
		f.err = L("A provider needs a name and at least one model.")
		return false
	}
	p := store.Provider{ID: f.id, Name: strings.TrimSpace(f.name), API: apiID(f.api), BaseURL: strings.TrimSpace(f.base), APIKey: strings.TrimSpace(f.key), Models: models,
		Options: map[string]bool{"replay_reasoning": f.replay, "encrypted_reasoning": f.encrypted, "web_search": f.web && apiID(f.api) != connect.Chat}}
	p, err := a.st.SaveProvider(p)
	if err != nil {
		f.err = err.Error()
		return false
	}
	f.id, f.err = p.ID, ""
	if a.st.Setting(team.SettingDefaultProvider) == "" {
		a.st.SetSetting(team.SettingDefaultProvider, p.ID)
	}
	return true
}

// confirmView asks before a delete, as an alert.
func (a *app) confirmView(c *ui.Context) {
	f := a.confirm
	if ui.AlertDialog(c, &f.open, f.title, f.message, L("Cancel"), f.action) == 1 {
		f.run()
	}
}

// sheets draws whichever sheet is up.
func (a *app) sheets(c *ui.Context) {
	if a.botSheet != nil {
		a.botSheetView(c)
		if !a.botSheet.open {
			a.botSheet = nil
		}
	}
	if a.groupSheet != nil {
		a.groupSheetView(c)
		if !a.groupSheet.open {
			a.groupSheet = nil
		}
	}
	if a.confirm != nil {
		a.confirmView(c)
		if !a.confirm.open {
			a.confirm = nil
		}
	}
	if a.routine != nil {
		a.routineSheetView(c)
		if a.routine != nil && !a.routine.open {
			a.routine = nil
		}
	}
	if a.memSheet != nil {
		a.memorySheetView(c)
		if !a.memSheet.open {
			a.memSheet = nil
		}
	}
	if a.palette != nil {
		a.paletteView(c)
	}
}

func (a *app) hasSheet() bool {
	return a.botSheet != nil || a.groupSheet != nil || a.confirm != nil || a.palette != nil || a.memSheet != nil || a.routine != nil
}
