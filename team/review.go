package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/store"
)

const reviewSystem = `You review one action an assistant wants to take on the user's own computer, before it runs. Answer with JSON only: {"decision": "allow" | "ask", "reason": "<one short sentence>"}.

Allow what is safe to do without asking: reading, listing, searching, building, testing, formatting, and changes inside the assistant's workspace that follow from what the user asked.
Ask when it could do harm or surprise the user: deleting or overwriting outside the workspace, rm -rf, sudo, changing system settings, installing software, sending messages, emails or posts, git push, publishing, payments, uploading files or secrets, anything the user's request does not call for, or when you cannot tell.`

// Review asks the bot's model whether an action may run without the user:
// allow, or ask with a reason. It asks when the model cannot answer.
func Review(ctx context.Context, model agent.Model, b store.Bot, request, action, workspace string) (allow bool, reason string) {
	prompt := fmt.Sprintf("The user's request: %s\n\nThe assistant (%s) works in %s and wants to run:\n%s", oneLine(request, 1500), b.Name, workspace, action)
	reply := model.Call(ctx, reviewSystem, []agent.Message{textMessage(prompt)}, nil, func(agent.Event) {})
	text := strings.TrimSpace(textOf(reply))
	if i, j := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}'); i >= 0 && j > i {
		text = text[i : j+1]
	}
	var v struct{ Decision, Reason string }
	if reply.StopReason == agent.StopError || json.Unmarshal([]byte(text), &v) != nil {
		return false, "the review could not decide"
	}
	return strings.EqualFold(v.Decision, "allow"), strings.TrimSpace(v.Reason)
}

// LastRequest is the newest thing the user wrote in a chat, for a review.
func (t *Team) LastRequest(chatID string) string {
	es, err := t.Store.Messages(chatID, 0)
	if err != nil {
		return ""
	}
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].Author == store.User && es[i].Msg.Role == agent.RoleUser {
			return textOf(es[i].Msg)
		}
	}
	return ""
}
