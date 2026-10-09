// Package anthropic is the adapter for the streaming Messages API
// (POST {base}/v1/messages). Any server that speaks it works: Anthropic,
// DeepSeek's /anthropic endpoint, a proxy.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/internal/wire"
)

const (
	defaultBase      = "https://api.anthropic.com"
	defaultMaxTokens = 16384
	maxRetries       = 2
	maxMarks         = 4 // cache_control marks one request may carry
)

// Provider implements provider.Provider.
type Provider struct {
	id, model string
	base      string
	apiKey    string
	client    *http.Client

	// ServerTools are declared before the function tools, e.g.
	// {"type":"web_search_20260209","name":"web_search"}.
	ServerTools []json.RawMessage
	// Budget selects `thinking: {type: enabled, budget_tokens}` (Haiku 4.5 and
	// earlier) instead of adaptive thinking.
	Budget bool
	// MaxRetryDelay caps a server-requested wait before a retry fails instead.
	MaxRetryDelay time.Duration
	// Backoff is the first retry wait; it doubles up to 8 s.
	Backoff time.Duration
	// NoCache turns the cache_control marks off.
	NoCache bool
	// NoImages replaces images with a note, for a text-only model.
	NoImages bool
}

// New returns a provider for the Messages API at base ("" is Anthropic's).
func New(id, base, apiKey, model string) *Provider {
	if base == "" {
		base = defaultBase
	}
	if id == "" {
		id = "anthropic"
	}
	return &Provider{
		id: id, model: model, apiKey: apiKey,
		base:          strings.TrimRight(base, "/"),
		client:        &http.Client{},
		MaxRetryDelay: 60 * time.Second,
		Backoff:       500 * time.Millisecond,
	}
}

func (p *Provider) ID() string    { return p.id }
func (p *Provider) Model() string { return p.model }

var _ provider.Provider = (*Provider)(nil)

// Stream sends req and streams the reply. It never fails the call: errors end
// the stream with a done event whose message has StopError or StopAborted.
func (p *Provider) Stream(ctx context.Context, req provider.Request) <-chan provider.Event {
	out := make(chan provider.Event, 16)
	go func() {
		defer close(out)
		msg := p.run(ctx, req, func(e provider.Event) { out <- e })
		out <- provider.Event{Kind: provider.EventDone, Message: &msg}
	}()
	return out
}

func (p *Provider) fail(ctx context.Context, partial agent.Message, err error) agent.Message {
	return wire.Fail(ctx, partial, p.id, p.model, err)
}

func (p *Provider) run(ctx context.Context, req provider.Request, emit func(provider.Event)) agent.Message {
	body, err := json.Marshal(p.buildBody(req))
	if err != nil {
		return p.fail(ctx, agent.Message{}, err)
	}
	resp, err := p.post(ctx, body)
	if err != nil {
		return p.fail(ctx, agent.Message{}, err)
	}
	defer resp.Body.Close()
	emit(provider.Event{Kind: provider.EventStart})

	s := &streamState{p: p, emit: emit}
	if err := s.read(resp.Body); err != nil {
		return p.fail(ctx, s.msg, err)
	}
	if s.errMsg != "" {
		return p.fail(ctx, s.msg, errors.New(s.errMsg))
	}
	if !s.stopped {
		return p.fail(ctx, s.msg, errors.New("stream ended before message_stop"))
	}
	s.msg.Role = agent.RoleAssistant
	s.msg.Provider, s.msg.Model = p.id, p.model
	return s.msg
}

func (p *Provider) post(ctx context.Context, body []byte) (*http.Response, error) {
	headers := map[string]string{"x-api-key": p.apiKey, "anthropic-version": "2023-06-01"}
	return wire.Post(ctx, p.client, p.messagesURL(), headers, body, wire.Retry{Max: maxRetries, Backoff: p.Backoff, MaxDelay: p.MaxRetryDelay})
}

func (p *Provider) messagesURL() string {
	if strings.HasSuffix(p.base, "/v1") {
		return p.base + "/messages"
	}
	return p.base + "/v1/messages"
}

// ---- request ----

type block = map[string]any

type wireMessage struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}

// callID gives a tool call id from another model the shape this API wants.
func callID(id string) string { return wire.CallID(id) }

func (p *Provider) buildBody(req provider.Request) map[string]any {
	transformed, origins := agent.Transform(req.Messages, agent.TransformOptions{Provider: p.id, Model: p.model, SupportsImages: !p.NoImages})
	msgs, ends := convert(transformed)

	marks := 0
	mark := func(m *wireMessage) {
		if p.NoCache || marks >= maxMarks || len(m.Content) == 0 {
			return
		}
		b := m.Content[len(m.Content)-1]
		if _, ok := b["cache_control"]; ok {
			return
		}
		if t, _ := b["type"].(string); t == "thinking" || t == "redacted_thinking" {
			return
		}
		b["cache_control"] = block{"type": "ephemeral"}
		marks++
	}

	body := map[string]any{
		"model":      p.model,
		"stream":     true,
		"max_tokens": defaultMaxTokens,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if strings.TrimSpace(req.System) != "" {
		sys := block{"type": "text", "text": req.System}
		if !p.NoCache {
			sys["cache_control"] = block{"type": "ephemeral"}
			marks++
		}
		body["system"] = []block{sys}
	}

	// The last user block, then the newest cache points, within the mark budget.
	if n := len(msgs); n > 0 && msgs[n-1].Role == "user" {
		mark(&msgs[n-1])
	}
	for i := len(req.CachePoints) - 1; i >= 0; i-- {
		if cp := agent.PrefixLen(origins, req.CachePoints[i]); cp > 0 && cp <= len(ends) {
			mark(&msgs[ends[cp-1]])
		}
	}
	body["messages"] = msgs

	var tools []any
	for _, t := range p.ServerTools {
		tools = append(tools, t)
	}
	for _, t := range req.Tools {
		tools = append(tools, block{
			"name": t.Name, "description": t.Description,
			"input_schema":          json.RawMessage(t.Parameters),
			"eager_input_streaming": true,
		})
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	p.applyThinking(body, req)
	return body
}

func (p *Provider) applyThinking(body map[string]any, req provider.Request) {
	level := req.Thinking
	if level == provider.ThinkingDefault {
		return
	}
	if p.Budget {
		budgets := map[provider.ThinkingLevel]int{
			provider.ThinkingLow: 1024, provider.ThinkingMedium: 2048, provider.ThinkingHigh: 8192,
		}
		b, ok := budgets[level]
		if !ok { // off
			return
		}
		body["thinking"] = block{"type": "enabled", "budget_tokens": b}
		if body["max_tokens"].(int) < b+1024 {
			body["max_tokens"] = b + 1024
		}
		return
	}
	if level == provider.ThinkingOff {
		body["thinking"] = block{"type": "disabled"}
		return
	}
	body["thinking"] = block{"type": "adaptive"}
	body["output_config"] = block{"effort": string(level)}
}

// convert maps the transcript to wire messages. Consecutive same-role messages
// merge so a tool result follows its call in the next message; assistant
// messages that end up empty are left out. ends[i] is the index in the result
// of the message that holds original message i's last block (or -1).
func convert(in []agent.Message) ([]wireMessage, []int) {
	var out []wireMessage
	ends := make([]int, len(in))
	for i, m := range in {
		role, blocks := "user", []block(nil)
		switch m.Role {
		case agent.RoleAssistant:
			role, blocks = "assistant", assistantBlocks(m)
		case agent.RoleTool:
			blocks = toolResultBlocks(m)
		default:
			blocks = userBlocks(m)
		}
		if len(blocks) == 0 {
			ends[i] = len(out) - 1
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
		} else {
			out = append(out, wireMessage{Role: role, Content: blocks})
		}
		ends[i] = len(out) - 1
	}
	for i, e := range ends {
		if e < 0 {
			ends[i] = 0
		}
	}
	return out, ends
}

func userBlocks(m agent.Message) []block {
	var bs []block
	for _, p := range m.Parts {
		switch p.Kind {
		case agent.PartText:
			if p.Text != "" {
				bs = append(bs, block{"type": "text", "text": p.Text})
			}
		case agent.PartImage:
			bs = append(bs, imageBlock(p))
		}
	}
	return bs
}

func imageBlock(p agent.Part) block {
	return block{"type": "image", "source": block{
		"type": "base64", "media_type": p.MediaType, "data": encodeB64(p.Data),
	}}
}

func toolResultBlocks(m agent.Message) []block {
	var bs []block
	for _, p := range m.Parts {
		if p.Kind != agent.PartToolResult {
			continue
		}
		b := block{"type": "tool_result", "tool_use_id": callID(p.CallID), "content": p.Text}
		if p.IsError {
			b["is_error"] = true
		}
		bs = append(bs, b)
	}
	return bs
}

func assistantBlocks(m agent.Message) []block {
	var bs []block
	for _, p := range m.Parts {
		switch p.Kind {
		case agent.PartText:
			if strings.TrimSpace(p.Text) != "" {
				bs = append(bs, block{"type": "text", "text": p.Text})
			}
		case agent.PartThinking:
			if p.Signature != "" {
				bs = append(bs, block{"type": "thinking", "thinking": p.Text, "signature": p.Signature})
			} else if strings.TrimSpace(p.Text) != "" { // cut off: goes as text
				bs = append(bs, block{"type": "text", "text": p.Text})
			}
		case agent.PartToolCall:
			args := p.Args
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			bs = append(bs, block{"type": "tool_use", "id": callID(p.CallID), "name": p.Name, "input": args})
		case agent.PartServer:
			var v block
			if json.Unmarshal(p.Raw, &v) == nil {
				bs = append(bs, v)
			}
		}
	}
	return bs
}

// ---- stream ----

type streamState struct {
	p       *Provider
	emit    func(provider.Event)
	msg     agent.Message
	blocks  map[int]*openBlock
	stopped bool
	errMsg  string
	stop    string
	details string
}

type openBlock struct {
	typ       string
	raw       json.RawMessage // content_block as started
	text      strings.Builder // text, thinking, or partial input JSON
	signature string
	id, name  string
}

type sseEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`

	Message *struct {
		ID    string    `json:"id"`
		Usage wireUsage `json:"usage"`
	} `json:"message"`

	ContentBlock json.RawMessage `json:"content_block"`

	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
		StopDetails *struct {
			Explanation string `json:"explanation"`
		} `json:"stop_details"`
	} `json:"delta"`

	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	OutputTokensDetails      struct {
		ThinkingTokens int `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func (s *streamState) addUsage(u wireUsage) {
	// message_start carries input; message_delta carries cumulative output.
	if u.InputTokens > 0 {
		s.msg.Usage.Input = u.InputTokens
	}
	if u.CacheReadInputTokens > 0 {
		s.msg.Usage.CacheRead = u.CacheReadInputTokens
	}
	if u.CacheCreationInputTokens > 0 {
		s.msg.Usage.CacheWrite = u.CacheCreationInputTokens
	}
	if u.OutputTokens > 0 {
		s.msg.Usage.Output = u.OutputTokens
	}
	if u.OutputTokensDetails.ThinkingTokens > 0 {
		s.msg.Usage.Reasoning = u.OutputTokensDetails.ThinkingTokens
	}
}

func (s *streamState) read(r io.Reader) error {
	s.blocks = map[int]*openBlock{}
	return wire.ReadSSE(r, func(_, data string) (bool, error) {
		if err := s.handle(data); err != nil {
			return false, err
		}
		return s.stopped || s.errMsg != "", nil
	})
}

func (s *streamState) handle(data string) error {
	if data == "" || data == "[DONE]" {
		return nil
	}
	var ev sseEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return fmt.Errorf("bad stream event: %w", err)
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			s.msg.ID = ev.Message.ID
			s.addUsage(ev.Message.Usage)
		}
	case "content_block_start":
		var cb struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Text      string          `json:"text"`
			Thinking  string          `json:"thinking"`
			Signature string          `json:"signature"`
			Input     json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(ev.ContentBlock, &cb)
		ob := &openBlock{typ: cb.Type, raw: ev.ContentBlock, id: cb.ID, name: cb.Name, signature: cb.Signature}
		ob.text.WriteString(cb.Text + cb.Thinking)
		s.blocks[ev.Index] = ob
	case "content_block_delta":
		ob := s.blocks[ev.Index]
		if ob == nil {
			return nil
		}
		switch ev.Delta.Type {
		case "text_delta":
			ob.text.WriteString(ev.Delta.Text)
			s.emit(provider.Event{Kind: provider.EventTextDelta, Delta: ev.Delta.Text})
		case "thinking_delta":
			ob.text.WriteString(ev.Delta.Thinking)
			s.emit(provider.Event{Kind: provider.EventThinkingDelta, Delta: ev.Delta.Thinking})
		case "signature_delta":
			ob.signature += ev.Delta.Signature
		case "input_json_delta":
			ob.text.WriteString(ev.Delta.PartialJSON)
		}
	case "content_block_stop":
		if ob := s.blocks[ev.Index]; ob != nil {
			s.finishBlock(ob)
			delete(s.blocks, ev.Index)
		}
	case "message_delta":
		s.stop = ev.Delta.StopReason
		if ev.Delta.StopDetails != nil {
			s.details = ev.Delta.StopDetails.Explanation
		}
		if ev.Usage != nil {
			s.addUsage(*ev.Usage)
		}
	case "message_stop":
		s.stopped = true
		return s.setStop()
	case "error":
		s.errMsg = "stream error"
		if ev.Error != nil && ev.Error.Message != "" {
			s.errMsg = ev.Error.Message
		}
	}
	return nil
}

func (s *streamState) finishBlock(ob *openBlock) {
	switch ob.typ {
	case "text":
		if ob.text.Len() > 0 {
			s.msg.Parts = append(s.msg.Parts, agent.Part{Kind: agent.PartText, Text: ob.text.String()})
		}
	case "thinking":
		s.msg.Parts = append(s.msg.Parts, agent.Part{Kind: agent.PartThinking, Text: ob.text.String(), Signature: ob.signature})
	case "tool_use":
		part := agent.Part{Kind: agent.PartToolCall, CallID: ob.id, Name: ob.name, Args: inputJSON(ob.text.String())}
		s.msg.Parts = append(s.msg.Parts, part)
		s.emit(provider.Event{Kind: provider.EventToolCall, Part: &part})
	case "server_tool_use":
		// Rebuild the block with its streamed input so it replays verbatim.
		var v block
		_ = json.Unmarshal(ob.raw, &v)
		v["input"] = inputJSON(ob.text.String())
		raw, _ := json.Marshal(v)
		s.msg.Parts = append(s.msg.Parts, agent.Part{Kind: agent.PartServer, Name: ob.name, Raw: raw})
	default: // *_tool_result, redacted_thinking, anything new: kept as is
		s.msg.Parts = append(s.msg.Parts, agent.Part{Kind: agent.PartServer, Name: ob.typ, Raw: ob.raw})
	}
}

func inputJSON(s string) json.RawMessage { return wire.Args(s) }

func (s *streamState) setStop() error {
	switch s.stop {
	case "end_turn", "stop_sequence", "pause_turn":
		s.msg.StopReason = agent.StopEnd
	case "max_tokens":
		s.msg.StopReason = agent.StopLength
	case "tool_use":
		s.msg.StopReason = agent.StopToolUse
	case "refusal":
		msg := "the model refused"
		if s.details != "" {
			msg += ": " + s.details
		}
		s.errMsg = msg
	case "":
		s.errMsg = "no stop reason before message_stop"
	default:
		s.errMsg = "stopped: " + s.stop
	}
	return nil
}
