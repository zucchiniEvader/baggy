package main

import (
	"fmt"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider/catalog"
	"github.com/spike/gobot/store"
	"github.com/spike/gobot/team"
)

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
	if a.sel == "" || !a.focused {
		return
	}
	a.st.MarkRead(a.sel)
	if a.unread[a.sel] > 0 {
		a.unread[a.sel] = 0
		a.showBadge()
	}
}

func (a *app) countUnread() {
	a.unread = map[string]int{}
	for _, ch := range a.chats {
		a.unread[ch.ID] = a.st.Unread(ch.ID)
	}
	a.showBadge()
}

// showBadge puts the unread total on the app's icon (the Dock's), zero
// clearing it.
func (a *app) showBadge() {
	total := 0
	for _, n := range a.unread {
		total += n
	}
	if a.setBadge != nil && total != a.badge {
		a.setBadge(total)
	}
	a.badge = total
}

// notifyEnd tells the user about a run that ended out of sight: the bot's
// last reply in it, or its failure; a run that added nothing unread (a
// PASS) tells nothing, whatever older replies wait unread.
func (a *app) notifyEnd(chatID string) {
	if a.notify == nil || a.watching(chatID) || a.unread[chatID] == 0 {
		return
	}
	ch := a.chat(chatID)
	if ch == nil {
		return
	}
	es, _ := a.st.Messages(chatID, max(ch.ReadSeq, a.runFrom[chatID]))
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
		title := a.name(es[i].Author)
		// A routine's run: its name beside the bot's, from the run's marker.
		for j := i - 1; j >= 0; j-- {
			if es[j].Author == store.User {
				break
			}
			if es[j].Meta.Routine != "" {
				title = L("%s · %s", title, es[j].Meta.RoutineName)
				break
			}
		}
		a.notify(title, subtitle, oneLine(body, 280), chatID)
		return
	}
}

// notifyNotice tells the user about a notice out of sight, as a routine
// paused while they were away.
func (a *app) notifyNotice(e store.Entry) {
	if a.notify == nil || a.watching(e.ChatID) || e.Msg.Kind != team.KindNotice {
		return
	}
	title := L("gobot")
	if ch := a.chat(e.ChatID); ch != nil {
		title = a.title(*ch)
	}
	a.notify(title, "", oneLine(messageText(e.Msg), 280), e.ChatID)
}
