package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
	"github.com/spike/gobot/provider/internal/wire"
)

// Responses implements provider.Provider over the streaming Responses API,
// stateless (`store: false`): every request carries the whole transcript.
type Responses struct {
	id, model string
	base      string
	apiKey    string
	client    *http.Client

	Retry wire.Retry
	// NoImages replaces images with a note, for a text-only model.
	NoImages bool
	// EncryptedReasoning asks for `reasoning.encrypted_content` and sends this
	// model's own reasoning items back, so its reasoning carries across tool
	// calls. Off by default: not every Responses server takes `include`.
	EncryptedReasoning bool
	// WebSearch declares OpenAI's server-side web_search tool; its calls are
	// kept to show, not sent back.
	WebSearch bool
}

// NewResponses returns a provider for the Responses API at base ("" is
// OpenAI's https://api.openai.com/v1).
func NewResponses(id, baseURL, apiKey, model string) *Responses {
	if id == "" {
		id = "openai"
	}
	return &Responses{id: id, model: model, base: base(baseURL), apiKey: apiKey, client: &http.Client{}, Retry: wire.DefaultRetry}
}

func (r *Responses) ID() string    { return r.id }
func (r *Responses) Model() string { return r.model }

var _ provider.Provider = (*Responses)(nil)

func (r *Responses) Stream(ctx context.Context, req provider.Request) <-chan provider.Event {
	out := make(chan provider.Event, 16)
	go func() {
		defer close(out)
		msg := r.run(ctx, req, func(e provider.Event) { out <- e })
		out <- provider.Event{Kind: provider.EventDone, Message: &msg}
	}()
	return out
}

func (r *Responses) run(ctx context.Context, req provider.Request, emit func(provider.Event)) agent.Message {
	fail := func(m agent.Message, err error) agent.Message { return wire.Fail(ctx, m, r.id, r.model, err) }
	body, err := json.Marshal(r.buildBody(req))
	if err != nil {
		return fail(agent.Message{}, err)
	}
	resp, err := wire.Post(ctx, r.client, r.base+"/responses", bearer(r.apiKey), body, r.Retry)
	if err != nil {
		return fail(agent.Message{}, err)
	}
	defer resp.Body.Close()
	emit(provider.Event{Kind: provider.EventStart})

	s := &respStream{emit: emit, items: map[string]*respItem{}}
	if err := wire.ReadSSE(resp.Body, s.handle); err != nil {
		return fail(s.message(), err)
	}
	msg := s.message()
	if s.errMsg != "" {
		return fail(msg, errors.New(s.errMsg))
	}
	if s.stop == "" {
		return fail(msg, errors.New("stream ended before response.completed"))
	}
	msg.StopReason = s.stop
	if s.stop == agent.StopEnd {
		for _, p := range msg.Parts {
			if p.Kind == agent.PartToolCall {
				msg.StopReason = agent.StopToolUse
				break
			}
		}
	}
	for i := range msg.Parts {
		if p := &msg.Parts[i]; p.Kind == agent.PartToolCall {
			emit(provider.Event{Kind: provider.EventToolCall, Part: p})
		}
	}
	msg.Role = agent.RoleAssistant
	msg.Provider, msg.Model = r.id, r.model
	return msg
}

// ---- request ----

func (r *Responses) buildBody(req provider.Request) obj {
	transformed, _ := agent.Transform(req.Messages, agent.TransformOptions{Provider: r.id, Model: r.model, SupportsImages: !r.NoImages})
	body := obj{
		"model":  r.model,
		"input":  r.input(transformed),
		"store":  false,
		"stream": true,
	}
	if strings.TrimSpace(req.System) != "" {
		body["instructions"] = req.System
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	switch req.Thinking {
	case provider.ThinkingLow, provider.ThinkingMedium, provider.ThinkingHigh:
		body["reasoning"] = obj{"effort": string(req.Thinking), "summary": "auto"}
	}
	if r.EncryptedReasoning {
		body["include"] = []string{"reasoning.encrypted_content"}
	}
	if len(req.Tools) > 0 || r.WebSearch {
		var tools []obj
		if r.WebSearch {
			tools = append(tools, obj{"type": "web_search"})
		}
		for _, t := range req.Tools {
			tools = append(tools, obj{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters, "strict": false})
		}
		body["tools"] = tools
		body["tool_choice"] = "auto"
		body["parallel_tool_calls"] = true
	}
	return body
}

// input maps the transcript to input items. An assistant message keeps its
// order: text runs become `output_text` messages between its reasoning items
// and `function_call`s.
func (r *Responses) input(msgs []agent.Message) []obj {
	var items []obj
	for _, m := range msgs {
		switch m.Role {
		case agent.RoleAssistant:
			var text strings.Builder
			flush := func() {
				if strings.TrimSpace(text.String()) != "" {
					items = append(items, obj{"type": "message", "role": "assistant",
						"content": []obj{{"type": "output_text", "text": text.String()}}})
				}
				text.Reset()
			}
			for _, p := range m.Parts {
				switch p.Kind {
				case agent.PartText:
					text.WriteString(p.Text)
				case agent.PartThinking:
					// Only a reasoning item with its encrypted content goes back.
					if r.EncryptedReasoning && p.Signature != "" && len(p.Raw) > 0 {
						flush()
						var v obj
						if json.Unmarshal(p.Raw, &v) == nil {
							items = append(items, v)
						}
					}
				case agent.PartToolCall:
					flush()
					items = append(items, obj{"type": "function_call", "call_id": wire.CallID(p.CallID), "name": p.Name, "arguments": argsString(p.Args)})
				}
			}
			flush()
		case agent.RoleTool:
			for _, p := range m.Parts {
				if p.Kind == agent.PartToolResult {
					items = append(items, obj{"type": "function_call_output", "call_id": wire.CallID(p.CallID), "output": resultText(p)})
				}
			}
		default:
			var content []obj
			for _, p := range m.Parts {
				switch p.Kind {
				case agent.PartText:
					content = append(content, obj{"type": "input_text", "text": p.Text})
				case agent.PartImage:
					content = append(content, obj{"type": "input_image", "image_url": dataURL(p)})
				}
			}
			if len(content) > 0 {
				items = append(items, obj{"type": "message", "role": "user", "content": content})
			}
		}
	}
	return items
}

// ---- stream ----

type respStream struct {
	emit   func(provider.Event)
	id     string
	order  []*respItem
	items  map[string]*respItem
	usage  agent.Usage
	stop   agent.StopReason
	errMsg string
}

type respItem struct {
	typ          string
	text         strings.Builder // message text, reasoning summary, or arguments
	callID, name string
	sawArgs      bool
	raw          json.RawMessage // the finished reasoning item
	encrypted    string
}

type respEvent struct {
	Type     string          `json:"type"`
	ItemID   string          `json:"item_id"`
	Delta    string          `json:"delta"`
	Item     json.RawMessage `json:"item"`
	Message  string          `json:"message"`
	Response *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			InputTokens        int `json:"input_tokens"`
			OutputTokens       int `json:"output_tokens"`
			InputTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	} `json:"response"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type wireItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	EncryptedContent string `json:"encrypted_content"`
}

func (s *respStream) handle(event, data string) (bool, error) {
	if data == "" || data == "[DONE]" {
		return data == "[DONE]", nil
	}
	var ev respEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return false, errors.New("bad stream event: " + err.Error())
	}
	if ev.Type == "" {
		ev.Type = event
	}
	switch ev.Type {
	case "response.created":
		if ev.Response != nil {
			s.id = ev.Response.ID
		}
	case "response.output_item.added":
		var w wireItem
		_ = json.Unmarshal(ev.Item, &w)
		it := &respItem{typ: w.Type, callID: w.CallID, name: w.Name}
		s.items[w.ID] = it
		s.order = append(s.order, it)
	case "response.output_text.delta":
		if it := s.items[ev.ItemID]; it != nil {
			it.text.WriteString(ev.Delta)
		}
		s.emit(provider.Event{Kind: provider.EventTextDelta, Delta: ev.Delta})
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if it := s.items[ev.ItemID]; it != nil {
			it.text.WriteString(ev.Delta)
		}
		s.emit(provider.Event{Kind: provider.EventThinkingDelta, Delta: ev.Delta})
	case "response.reasoning_summary_part.done":
		if it := s.items[ev.ItemID]; it != nil && it.text.Len() > 0 {
			it.text.WriteString("\n\n")
		}
	case "response.function_call_arguments.delta":
		if it := s.items[ev.ItemID]; it != nil {
			it.sawArgs = true
			it.text.WriteString(ev.Delta)
		}
	case "response.output_item.done":
		var w wireItem
		_ = json.Unmarshal(ev.Item, &w)
		it := s.items[w.ID]
		if it == nil {
			return false, nil
		}
		switch w.Type {
		case "function_call":
			if !it.sawArgs {
				it.text.WriteString(w.Arguments)
			}
			if w.CallID != "" {
				it.callID = w.CallID
			}
		case "reasoning":
			if w.EncryptedContent != "" {
				it.raw, it.encrypted = ev.Item, w.EncryptedContent
			}
		case "web_search_call":
			it.raw = ev.Item
			part := agent.Part{Kind: agent.PartServer, Name: w.Type, Raw: ev.Item}
			s.emit(provider.Event{Kind: provider.EventServerTool, Part: &part})
		}
	case "response.completed", "response.done", "response.incomplete":
		s.stop = agent.StopEnd
		if ev.Type == "response.incomplete" {
			s.stop = agent.StopLength
		}
		if resp := ev.Response; resp != nil {
			if resp.Status == "incomplete" {
				s.stop = agent.StopLength
			}
			if u := resp.Usage; u != nil {
				// input_tokens counts the cached tokens too; Input is the part read fresh.
				cached := u.InputTokensDetails.CachedTokens
				s.usage = agent.Usage{Input: max(u.InputTokens-cached, 0), Output: u.OutputTokens, CacheRead: cached, Reasoning: u.OutputTokensDetails.ReasoningTokens}
			}
		}
		return true, nil
	case "response.failed":
		s.errMsg = "response failed"
		if ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Message != "" {
			s.errMsg = ev.Response.Error.Message
		}
		return true, nil
	case "error":
		s.errMsg = ev.Message
		if s.errMsg == "" && ev.Error != nil {
			s.errMsg = ev.Error.Message
		}
		if s.errMsg == "" {
			s.errMsg = "stream error"
		}
		return true, nil
	}
	return false, nil
}

// message is what the stream has said so far, item by item.
func (s *respStream) message() agent.Message {
	m := agent.Message{ID: s.id, Usage: s.usage}
	for _, it := range s.order {
		switch it.typ {
		case "message":
			if it.text.Len() > 0 {
				m.Parts = append(m.Parts, agent.Part{Kind: agent.PartText, Text: it.text.String()})
			}
		case "reasoning":
			text := strings.TrimSpace(it.text.String())
			if text != "" || it.encrypted != "" {
				m.Parts = append(m.Parts, agent.Part{Kind: agent.PartThinking, Text: text, Signature: it.encrypted, Raw: it.raw})
			}
		case "function_call":
			m.Parts = append(m.Parts, agent.Part{Kind: agent.PartToolCall, CallID: it.callID, Name: it.name, Args: wire.Args(it.text.String())})
		case "web_search_call":
			if len(it.raw) > 0 {
				m.Parts = append(m.Parts, agent.Part{Kind: agent.PartServer, Name: it.typ, Raw: it.raw})
			}
		}
	}
	return m
}
