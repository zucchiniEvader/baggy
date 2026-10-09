package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
)

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "data: %s\n\n", e)
	}
	return b.String()
}

// server answers every request with body and records the last request.
func server(t *testing.T, status int, body string, got *map[string]any) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if got != nil {
			json.Unmarshal(b, got)
		}
		if r.Header.Get("authorization") != "Bearer k" {
			t.Errorf("bad auth: %q", r.Header.Get("authorization"))
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collect(p provider.Provider, req provider.Request) (agent.Message, []provider.Event) {
	var evs []provider.Event
	var final agent.Message
	for e := range p.Stream(context.Background(), req) {
		evs = append(evs, e)
		if e.Kind == provider.EventDone {
			final = *e.Message
		}
	}
	return final, evs
}

func text(role agent.Role, s string) agent.Message {
	return agent.Message{Role: role, Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
}

func count(evs []provider.Event, k provider.EventKind) int {
	n := 0
	for _, e := range evs {
		if e.Kind == k {
			n++
		}
	}
	return n
}

// ---- Chat Completions ----

func TestChatStreamsTextThinkingParallelCalls(t *testing.T) {
	var got map[string]any
	srv := server(t, 200, sse(
		`{"id":"c1","choices":[{"delta":{"reasoning_content":"hm"}}]}`,
		`{"id":"c1","choices":[{"delta":{"content":"Hel"}}]}`,
		`{"id":"c1","choices":[{"delta":{"content":"lo"}}]}`,
		`{"id":"c1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"echo","arguments":"{\"x\":"}}]}}]}`,
		`{"id":"c1","choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"echo","arguments":"{}"}}]}}]}`,
		`{"id":"c1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`{"id":"c1","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_cache_hit_tokens":80,"completion_tokens_details":{"reasoning_tokens":3}}}`,
		`[DONE]`,
	), &got)
	c := NewChat("", srv.URL, "k", "gpt-x")
	msg, evs := collect(c, provider.Request{
		System:    "be brief",
		Messages:  []agent.Message{text(agent.RoleUser, "hi")},
		Tools:     []provider.ToolSpec{{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}},
		MaxTokens: 50,
		Thinking:  provider.ThinkingHigh,
	})
	if msg.StopReason != agent.StopToolUse || msg.ID != "c1" || msg.Provider != "openai" {
		t.Fatalf("bad message: %+v", msg)
	}
	p := msg.Parts
	if len(p) != 4 || p[0].Kind != agent.PartThinking || p[1].Text != "Hello" ||
		p[2].CallID != "call_a" || string(p[2].Args) != `{"x":1}` || p[3].CallID != "call_b" {
		t.Fatalf("bad parts: %+v", p)
	}
	if u := msg.Usage; u.Input != 20 || u.CacheRead != 80 || u.Output != 7 || u.Reasoning != 3 {
		t.Fatalf("bad usage: %+v", u)
	}
	if count(evs, provider.EventToolCall) != 2 || count(evs, provider.EventTextDelta) != 2 || count(evs, provider.EventThinkingDelta) != 1 {
		t.Fatalf("bad events: %+v", evs)
	}
	msgs := got["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["content"] != "hi" {
		t.Fatalf("bad messages: %v", msgs)
	}
	if got["max_tokens"] != 50.0 || got["reasoning_effort"] != "high" || got["stream_options"] == nil {
		t.Fatalf("bad body: %v", got)
	}
	tool := got["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "function" || tool["function"].(map[string]any)["name"] != "echo" {
		t.Fatalf("bad tools: %v", tool)
	}
}

func TestChatConvertsTranscript(t *testing.T) {
	c := NewChat("ds", "http://x", "k", "m")
	c.ReplayReasoning = true
	c.MaxTokensField = "max_completion_tokens"
	msgs := []agent.Message{
		{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "look"}, {Kind: agent.PartImage, MediaType: "image/png", Data: []byte{1}}}},
		{Role: agent.RoleAssistant, Provider: "ds", Model: "m", StopReason: agent.StopToolUse, Parts: []agent.Part{
			{Kind: agent.PartThinking, Text: "plan"},
			{Kind: agent.PartToolCall, CallID: "call:1", Name: "echo", Args: json.RawMessage(`{"a":1}`)},
			{Kind: agent.PartToolCall, CallID: "c2", Name: "echo", Args: json.RawMessage(`"not json"`)},
		}},
		{Role: agent.RoleTool, Parts: []agent.Part{
			{Kind: agent.PartToolResult, CallID: "call:1", Text: "ok"},
			{Kind: agent.PartToolResult, CallID: "c2", Text: ""},
		}},
		{Role: agent.RoleAssistant, Provider: "other", Model: "x", StopReason: agent.StopEnd, Parts: []agent.Part{
			{Kind: agent.PartThinking, Text: "foreign"}, {Kind: agent.PartText, Text: " done"},
		}},
	}
	body := c.buildBody(provider.Request{Messages: msgs, MaxTokens: 9})
	raw, _ := json.Marshal(body)
	var b struct {
		Messages []map[string]any `json:"messages"`
		MaxComp  int              `json:"max_completion_tokens"`
	}
	json.Unmarshal(raw, &b)
	if len(b.Messages) != 5 || b.MaxComp != 9 {
		t.Fatalf("bad body: %s", raw)
	}
	if parts, ok := b.Messages[0]["content"].([]any); !ok || len(parts) != 2 ||
		!strings.HasPrefix(parts[1].(map[string]any)["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,") {
		t.Fatalf("bad user content: %v", b.Messages[0])
	}
	a := b.Messages[1]
	calls := a["tool_calls"].([]any)
	if a["content"] != nil || a["reasoning_content"] != "plan" || len(calls) != 2 ||
		calls[0].(map[string]any)["id"] != "call_1" ||
		calls[1].(map[string]any)["function"].(map[string]any)["arguments"] != "not json" {
		t.Fatalf("bad assistant: %v", a)
	}
	if b.Messages[2]["tool_call_id"] != "call_1" || b.Messages[3]["content"] != NoToolOutput {
		t.Fatalf("bad tool messages: %v %v", b.Messages[2], b.Messages[3])
	}
	if b.Messages[4]["content"] != "foreign done" || b.Messages[4]["reasoning_content"] != nil {
		t.Fatalf("foreign thinking not text: %v", b.Messages[4])
	}

	c.ReplayReasoning = false
	raw, _ = json.Marshal(c.buildBody(provider.Request{Messages: msgs}))
	if strings.Contains(string(raw), "reasoning_content") || strings.Contains(string(raw), `"plan"`) {
		t.Fatalf("own thinking sent without ReplayReasoning: %s", raw)
	}
}

func TestChatInvalidArgumentsStayMarshalable(t *testing.T) {
	srv := server(t, 200, sse(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"echo","arguments":"{oops"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`), nil)
	msg, _ := collect(NewChat("", srv.URL, "k", "m"), provider.Request{})
	if _, err := json.Marshal(msg); err != nil || string(msg.Parts[0].Args) != `"{oops"` {
		t.Fatalf("args %s, marshal err %v", msg.Parts[0].Args, err)
	}
}

func TestChatEndsWithoutFinishIsError(t *testing.T) {
	srv := server(t, 200, sse(`{"choices":[{"delta":{"content":"part"}}]}`), nil)
	msg, _ := collect(NewChat("", srv.URL, "k", "m"), provider.Request{})
	if msg.StopReason != agent.StopError || msg.Parts[0].Text != "part" {
		t.Fatalf("bad message: %+v", msg)
	}
}

func TestChatHTTPError(t *testing.T) {
	srv := server(t, 401, `{"error":{"message":"bad key"}}`, nil)
	msg, _ := collect(NewChat("", srv.URL, "k", "m"), provider.Request{})
	if msg.StopReason != agent.StopError || !strings.Contains(msg.Parts[0].Text, "bad key") {
		t.Fatalf("bad message: %+v", msg)
	}
}

func cancelServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // the server notices a closed connection once the body is read
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCancelIsAborted(t *testing.T) {
	srv := cancelServer(t)
	for _, p := range []provider.Provider{NewChat("", srv.URL, "k", "m"), NewResponses("", srv.URL, "k", "m")} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		var final agent.Message
		for e := range p.Stream(ctx, provider.Request{}) {
			if e.Kind == provider.EventDone {
				final = *e.Message
			}
		}
		cancel()
		if final.StopReason != agent.StopAborted {
			t.Fatalf("%T: stop=%s", p, final.StopReason)
		}
	}
}

// ---- Responses ----

func TestResponsesStreamsReasoningTextCalls(t *testing.T) {
	var got map[string]any
	srv := server(t, 200, sse(
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_1"}}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","delta":"think"}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"think"}],"encrypted_content":"ENC"}}`,
		`{"type":"response.output_item.added","item":{"type":"message","id":"msg_1"}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","delta":"Hi"}`,
		`{"type":"response.output_item.done","item":{"type":"message","id":"msg_1"}}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_a","name":"echo"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"x\":1}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_a","name":"echo","arguments":"{\"x\":1}"}}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_2","call_id":"call_b","name":"echo"}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_2","call_id":"call_b","name":"echo","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":50,"output_tokens":9,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":4}}}}`,
	), &got)
	r := NewResponses("", srv.URL, "k", "gpt-x")
	r.EncryptedReasoning = true
	msg, evs := collect(r, provider.Request{
		System:   "be brief",
		Messages: []agent.Message{text(agent.RoleUser, "hi")},
		Tools:    []provider.ToolSpec{{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Thinking: provider.ThinkingMedium,
	})
	if msg.StopReason != agent.StopToolUse || msg.ID != "resp_1" {
		t.Fatalf("bad message: %+v", msg)
	}
	p := msg.Parts
	if len(p) != 4 || p[0].Text != "think" || p[0].Signature != "ENC" || len(p[0].Raw) == 0 || p[1].Text != "Hi" ||
		string(p[2].Args) != `{"x":1}` || p[3].CallID != "call_b" || string(p[3].Args) != `{}` {
		t.Fatalf("bad parts: %+v", p)
	}
	if u := msg.Usage; u.Input != 20 || u.CacheRead != 30 || u.Output != 9 || u.Reasoning != 4 {
		t.Fatalf("bad usage: %+v", u)
	}
	if count(evs, provider.EventToolCall) != 2 || count(evs, provider.EventThinkingDelta) != 1 {
		t.Fatalf("bad events: %+v", evs)
	}
	if got["instructions"] != "be brief" || got["store"] != false || got["tool_choice"] != "auto" {
		t.Fatalf("bad body: %v", got)
	}
	if re := got["reasoning"].(map[string]any); re["effort"] != "medium" || re["summary"] != "auto" {
		t.Fatalf("bad reasoning: %v", re)
	}
	if inc := got["include"].([]any); inc[0] != "reasoning.encrypted_content" {
		t.Fatalf("bad include: %v", inc)
	}
}

func TestResponsesInputReplaysOwnReasoning(t *testing.T) {
	r := NewResponses("oa", "http://x", "k", "m")
	r.EncryptedReasoning = true
	reasoning := json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"ENC"}`)
	msgs := []agent.Message{
		text(agent.RoleUser, "go"),
		{Role: agent.RoleAssistant, Provider: "oa", Model: "m", StopReason: agent.StopToolUse, Parts: []agent.Part{
			{Kind: agent.PartThinking, Text: "t", Signature: "ENC", Raw: reasoning},
			{Kind: agent.PartText, Text: "calling"},
			{Kind: agent.PartToolCall, CallID: "call:1", Name: "echo", Args: json.RawMessage(`{}`)},
		}},
		{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, CallID: "call:1", Text: "ok"}}},
	}
	raw, _ := json.Marshal(r.buildBody(provider.Request{Messages: msgs}))
	var b struct {
		Input []map[string]any `json:"input"`
	}
	json.Unmarshal(raw, &b)
	types := []string{}
	for _, it := range b.Input {
		types = append(types, fmt.Sprint(it["type"]))
	}
	if strings.Join(types, ",") != "message,reasoning,message,function_call,function_call_output" {
		t.Fatalf("bad items %v: %s", types, raw)
	}
	if b.Input[3]["call_id"] != "call_1" || b.Input[3]["id"] != nil || b.Input[4]["call_id"] != "call_1" {
		t.Fatalf("bad call ids: %s", raw)
	}

	r.EncryptedReasoning = false
	raw, _ = json.Marshal(r.buildBody(provider.Request{Messages: msgs}))
	if strings.Contains(string(raw), "ENC") {
		t.Fatalf("reasoning sent without EncryptedReasoning: %s", raw)
	}
}

func TestResponsesIncompleteAndFailed(t *testing.T) {
	srv := server(t, 200, sse(
		`{"type":"response.output_item.added","item":{"type":"message","id":"m"}}`,
		`{"type":"response.output_text.delta","item_id":"m","delta":"cut"}`,
		`{"type":"response.incomplete","response":{"status":"incomplete"}}`), nil)
	if msg, _ := collect(NewResponses("", srv.URL, "k", "m"), provider.Request{}); msg.StopReason != agent.StopLength || msg.Parts[0].Text != "cut" {
		t.Fatalf("incomplete: %+v", msg)
	}
	srv = server(t, 200, sse(`{"type":"response.failed","response":{"error":{"message":"quota"}}}`), nil)
	if msg, _ := collect(NewResponses("", srv.URL, "k", "m"), provider.Request{}); msg.StopReason != agent.StopError || !strings.Contains(msg.Parts[0].Text, "quota") {
		t.Fatalf("failed: %+v", msg)
	}
	srv = server(t, 200, sse(`{"type":"response.created","response":{"id":"r"}}`), nil)
	if msg, _ := collect(NewResponses("", srv.URL, "k", "m"), provider.Request{}); msg.StopReason != agent.StopError {
		t.Fatalf("no completed: %+v", msg)
	}
}
