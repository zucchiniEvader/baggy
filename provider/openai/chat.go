package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/zucchiniEvader/baggy/agent"
	"github.com/zucchiniEvader/baggy/provider"
	"github.com/zucchiniEvader/baggy/provider/internal/wire"
)

// Chat implements provider.Provider over streaming Chat Completions.
type Chat struct {
	id, model string
	base      string
	apiKey    string
	client    *http.Client

	Retry wire.Retry
	// NoImages replaces images with a note, for a text-only model.
	NoImages bool
	// ReplayReasoning sends this model's own thinking back as
	// `reasoning_content` on its assistant messages, which DeepSeek's thinking
	// mode wants within a turn of tool calls; other servers get no thinking.
	ReplayReasoning bool
	// MaxTokensField names the output cap: "max_tokens" (default) or
	// "max_completion_tokens", which OpenAI's reasoning models want.
	MaxTokensField string
}

// NewChat returns a provider for the Chat Completions API at base ("" is
// OpenAI's https://api.openai.com/v1).
func NewChat(id, baseURL, apiKey, model string) *Chat {
	if id == "" {
		id = "openai"
	}
	return &Chat{id: id, model: model, base: base(baseURL), apiKey: apiKey, client: &http.Client{}, Retry: wire.DefaultRetry}
}

func (c *Chat) ID() string    { return c.id }
func (c *Chat) Model() string { return c.model }

var _ provider.Provider = (*Chat)(nil)

func (c *Chat) Stream(ctx context.Context, req provider.Request) <-chan provider.Event {
	out := make(chan provider.Event, 16)
	go func() {
		defer close(out)
		msg := c.run(ctx, req, func(e provider.Event) { out <- e })
		out <- provider.Event{Kind: provider.EventDone, Message: &msg}
	}()
	return out
}

func (c *Chat) run(ctx context.Context, req provider.Request, emit func(provider.Event)) agent.Message {
	fail := func(m agent.Message, err error) agent.Message { return wire.Fail(ctx, m, c.id, c.model, err) }
	body, err := json.Marshal(c.buildBody(req))
	if err != nil {
		return fail(agent.Message{}, err)
	}
	resp, err := wire.Post(ctx, c.client, c.base+"/chat/completions", bearer(c.apiKey), body, c.Retry)
	if err != nil {
		return fail(agent.Message{}, err)
	}
	defer resp.Body.Close()
	emit(provider.Event{Kind: provider.EventStart})

	s := &chatStream{emit: emit, calls: map[int]*chatCall{}}
	if err := wire.ReadSSE(resp.Body, s.handle); err != nil {
		return fail(s.message(), err)
	}
	msg := s.message()
	if s.errMsg != "" {
		return fail(msg, errors.New(s.errMsg))
	}
	if s.finish == "" {
		return fail(msg, errors.New("stream ended without a finish_reason"))
	}
	switch {
	case s.finish == "length":
		msg.StopReason = agent.StopLength
	case s.finish == "tool_calls" || s.finish == "function_call" || len(s.calls) > 0:
		msg.StopReason = agent.StopToolUse
	case s.finish == "content_filter":
		return fail(msg, errors.New("stopped: content_filter"))
	default:
		msg.StopReason = agent.StopEnd
	}
	for i := range msg.Parts {
		if p := &msg.Parts[i]; p.Kind == agent.PartToolCall {
			emit(provider.Event{Kind: provider.EventToolCall, Part: p})
		}
	}
	msg.Role = agent.RoleAssistant
	msg.Provider, msg.Model = c.id, c.model
	return msg
}

// ---- request ----

type obj = map[string]any

func (c *Chat) buildBody(req provider.Request) obj {
	transformed, _ := agent.Transform(req.Messages, agent.TransformOptions{Provider: c.id, Model: c.model, SupportsImages: !c.NoImages})
	var msgs []obj
	if strings.TrimSpace(req.System) != "" {
		msgs = append(msgs, obj{"role": "system", "content": req.System})
	}
	for _, m := range transformed {
		msgs = append(msgs, c.convert(m)...)
	}
	body := obj{
		"model":          c.model,
		"messages":       msgs,
		"stream":         true,
		"stream_options": obj{"include_usage": true},
	}
	if req.MaxTokens > 0 {
		field := c.MaxTokensField
		if field == "" {
			field = "max_tokens"
		}
		body[field] = req.MaxTokens
	}
	switch req.Thinking {
	case provider.ThinkingLow, provider.ThinkingMedium, provider.ThinkingHigh:
		body["reasoning_effort"] = string(req.Thinking)
	}
	if len(req.Tools) > 0 {
		var tools []obj
		for _, t := range req.Tools {
			tools = append(tools, obj{"type": "function", "function": obj{
				"name": t.Name, "description": t.Description, "parameters": t.Parameters,
			}})
		}
		body["tools"] = tools
	}
	return body
}

// convert maps one transcript message to request messages: a tool message
// becomes one `tool` message per result.
func (c *Chat) convert(m agent.Message) []obj {
	switch m.Role {
	case agent.RoleAssistant:
		msg := obj{"role": "assistant", "content": nil}
		if t := assistantText(m); t != "" {
			msg["content"] = t
		}
		var reasoning strings.Builder
		var calls []obj
		for _, p := range m.Parts {
			switch p.Kind {
			case agent.PartThinking:
				reasoning.WriteString(p.Text)
			case agent.PartToolCall:
				calls = append(calls, obj{"id": wire.CallID(p.CallID), "type": "function", "function": obj{
					"name": p.Name, "arguments": argsString(p.Args),
				}})
			}
		}
		if c.ReplayReasoning && reasoning.Len() > 0 {
			msg["reasoning_content"] = reasoning.String()
		}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		if msg["content"] == nil && len(calls) == 0 {
			return nil
		}
		return []obj{msg}
	case agent.RoleTool:
		var out []obj
		for _, p := range m.Parts {
			if p.Kind == agent.PartToolResult {
				out = append(out, obj{"role": "tool", "tool_call_id": wire.CallID(p.CallID), "content": resultText(p)})
			}
		}
		return out
	default:
		var texts []string
		var parts []obj
		onlyText := true
		for _, p := range m.Parts {
			switch p.Kind {
			case agent.PartText:
				texts = append(texts, p.Text)
				parts = append(parts, obj{"type": "text", "text": p.Text})
			case agent.PartImage:
				onlyText = false
				parts = append(parts, obj{"type": "image_url", "image_url": obj{"url": dataURL(p)}})
			}
		}
		if len(parts) == 0 {
			return nil
		}
		if onlyText {
			return []obj{{"role": "user", "content": strings.Join(texts, "\n")}}
		}
		return []obj{{"role": "user", "content": parts}}
	}
}

// ---- stream ----

type chatStream struct {
	emit     func(provider.Event)
	id       string
	thinking strings.Builder
	text     strings.Builder
	calls    map[int]*chatCall
	usage    agent.Usage
	finish   string
	errMsg   string
}

type chatCall struct {
	id, name string
	args     strings.Builder
}

type chatChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens         int `json:"prompt_tokens"`
		CompletionTokens     int `json:"completion_tokens"`
		PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
		PromptTokensDetails  struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (s *chatStream) handle(_, data string) (bool, error) {
	if data == "[DONE]" {
		return true, nil
	}
	if data == "" {
		return false, nil
	}
	var ch chatChunk
	if err := json.Unmarshal([]byte(data), &ch); err != nil {
		return false, errors.New("bad stream chunk: " + err.Error())
	}
	if ch.Error != nil {
		s.errMsg = ch.Error.Message
		if s.errMsg == "" {
			s.errMsg = "stream error"
		}
		return true, nil
	}
	if s.id == "" {
		s.id = ch.ID
	}
	if u := ch.Usage; u != nil {
		// prompt_tokens counts the cached tokens too; Input is the part read fresh.
		cached := u.PromptCacheHitTokens
		if cached == 0 {
			cached = u.PromptTokensDetails.CachedTokens
		}
		s.usage = agent.Usage{
			Input: max(u.PromptTokens-cached, 0), Output: u.CompletionTokens,
			CacheRead: cached, Reasoning: u.CompletionTokensDetails.ReasoningTokens,
		}
	}
	if len(ch.Choices) == 0 {
		return false, nil
	}
	choice := ch.Choices[0]
	d := choice.Delta
	if r := d.ReasoningContent + d.Reasoning; r != "" {
		s.thinking.WriteString(r)
		s.emit(provider.Event{Kind: provider.EventThinkingDelta, Delta: r})
	}
	if d.Content != "" {
		s.text.WriteString(d.Content)
		s.emit(provider.Event{Kind: provider.EventTextDelta, Delta: d.Content})
	}
	for _, tc := range d.ToolCalls {
		call := s.calls[tc.Index]
		if call == nil {
			call = &chatCall{}
			s.calls[tc.Index] = call
		}
		if tc.ID != "" {
			call.id = tc.ID
		}
		if tc.Function.Name != "" {
			call.name = tc.Function.Name
		}
		call.args.WriteString(tc.Function.Arguments)
	}
	if choice.FinishReason != "" {
		s.finish = choice.FinishReason
	}
	return false, nil
}

// message is what the stream has said so far: thinking, text, then the calls
// in index order.
func (s *chatStream) message() agent.Message {
	m := agent.Message{ID: s.id, Usage: s.usage}
	if s.thinking.Len() > 0 {
		m.Parts = append(m.Parts, agent.Part{Kind: agent.PartThinking, Text: s.thinking.String()})
	}
	if s.text.Len() > 0 {
		m.Parts = append(m.Parts, agent.Part{Kind: agent.PartText, Text: s.text.String()})
	}
	idx := make([]int, 0, len(s.calls))
	for i := range s.calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		call := s.calls[i]
		id := call.id
		if id == "" {
			id = "call_" + strconv.Itoa(i)
		}
		m.Parts = append(m.Parts, agent.Part{Kind: agent.PartToolCall, CallID: id, Name: call.name, Args: wire.Args(call.args.String())})
	}
	return m
}
