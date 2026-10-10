package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider/catalog"
	"github.com/spike/gobot/store"
)

// avatar is a bot's look: its icon in a gray tile, size high.
func (a *app) avatar(c *ui.Context, botID string, size float32) {
	iconTile(c, botIcon(a.byID[botID].Icon), size).Label(a.name(botID))
}

// contextLine is how full the chat's last request was: tokens against the
// model's window.
func (a *app) contextLine(chatID string) string {
	es, err := a.st.Messages(chatID, 0)
	if err != nil {
		return ""
	}
	for i := len(es) - 1; i >= 0; i-- {
		m := es[i].Msg
		if m.Role == agent.RoleAssistant && m.Usage.Context() > 0 {
			used, window := m.Usage.Context(), catalog.Window(m.Model)
			return L("Context: %s of %s tokens (%d%%)", short(used), short(window), used*100/max(window, 1))
		}
	}
	return ""
}

func short(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

// ---- unread and notifications ----

// watching reports whether the user sees the chat: it is shown and the
// window has the focus.
func (a *app) watching(chatID string) bool { return a.sel == chatID && a.focused }

// markRead clears the chat shown, when the user sees it.
func (a *app) markRead() {
	if a.sel != "" && a.focused && a.unread[a.sel] > 0 {
		a.st.MarkRead(a.sel)
		a.unread[a.sel] = 0
	} else if a.sel != "" && a.focused {
		a.st.MarkRead(a.sel)
	}
}

func (a *app) countUnread() {
	a.unread = map[string]int{}
	for _, ch := range a.chats {
		a.unread[ch.ID] = a.st.Unread(ch.ID)
	}
}

// notifyEnd tells the user about a run that ended out of sight: the bot's
// last reply, or its failure.
func (a *app) notifyEnd(chatID string) {
	if a.notify == nil || a.watching(chatID) || a.unread[chatID] == 0 {
		return
	}
	ch := a.chat(chatID)
	if ch == nil {
		return
	}
	es, _ := a.st.Messages(chatID, ch.ReadSeq)
	for i := len(es) - 1; i >= 0; i-- {
		m := es[i].Msg
		if m.Role != agent.RoleAssistant || m.StopReason == agent.StopToolUse || es[i].Author == store.User {
			continue
		}
		body := messageText(m)
		if m.StopReason == agent.StopError {
			body = L("Failed: %s", errorText(m))
		}
		subtitle := ""
		if ch.Kind == store.Group {
			subtitle = ch.Title
		}
		a.notify(a.name(es[i].Author), subtitle, oneLine(body, 280), chatID)
		return
	}
}
