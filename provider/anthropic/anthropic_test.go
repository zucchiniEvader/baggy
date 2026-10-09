package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spike/gobot/agent"
	"github.com/spike/gobot/provider"
)

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "event: x\ndata: %s\n\n", e)
	}
	return b.String()
}

func collect(p *Provider, req provider.Request) (agent.Message, []provider.Event) {
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

func TestStreamTextThinkingToolCall(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("bad headers: %v", r.Header)
		}
		io.WriteString(w, sse(
			`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10,"cache_read_input_tokens":4}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hm"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hel"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"lo"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"1}"}}`,
			`{"type":"content_block_stop","index":2}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`,
			`{"type":"message_stop"}`,
		))
	}))
	defer srv.Close()

	p := New("", srv.URL, "k", "claude-x")
	msg, evs := collect(p, provider.Request{
		System:   "be brief",
		Messages: []agent.Message{{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "hi"}}}},
		Tools:    []provider.ToolSpec{{Name: "echo", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Thinking: provider.ThinkingHigh,
	})

	if msg.StopReason != agent.StopToolUse || msg.ID != "msg_1" || msg.Provider != "anthropic" {
		t.Fatalf("bad message: %+v", msg)
	}
	if len(msg.Parts) != 3 || msg.Parts[0].Signature != "sig" || msg.Parts[1].Text != "Hello" ||
		msg.Parts[2].Name != "echo" || string(msg.Parts[2].Args) != `{"x":1}` {
		t.Fatalf("bad parts: %+v", msg.Parts)
	}
	if u := msg.Usage; u.Input != 10 || u.CacheRead != 4 || u.Output != 7 {
		t.Fatalf("bad usage: %+v", u)
	}
	var sawCall bool
	for _, e := range evs {
		sawCall = sawCall || e.Kind == provider.EventToolCall
	}
	if !sawCall {
		t.Fatal("no tool_call event")
	}
	if got["thinking"].(map[string]any)["type"] != "adaptive" || got["output_config"].(map[string]any)["effort"] != "high" {
		t.Fatalf("bad thinking: %v", got["thinking"])
	}
	sys := got["system"].([]any)[0].(map[string]any)
	if sys["cache_control"] == nil {
		t.Fatal("system not marked for caching")
	}
}

func TestConvertMergesAndSealsReplay(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "a"}}},
		{Role: agent.RoleAssistant, Parts: []agent.Part{
			{Kind: agent.PartThinking, Text: "cut off"}, // no signature: goes as text
			{Kind: agent.PartServer, Raw: json.RawMessage(`{"type":"web_search_tool_result","tool_use_id":"s1","content":[]}`)},
			{Kind: agent.PartToolCall, CallID: "call:1", Name: "echo", Args: json.RawMessage(`{}`)},
		}},
		{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, CallID: "call:1", Text: "ok"}}},
		{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "b"}}},
		{Role: agent.RoleAssistant, Parts: []agent.Part{{Kind: agent.PartText, Text: "  "}}}, // empty: dropped
	}
	out, _ := convert(msgs)
	if len(out) != 3 || out[0].Role != "user" || out[1].Role != "assistant" || out[2].Role != "user" {
		t.Fatalf("bad roles: %+v", out)
	}
	if out[1].Content[0]["type"] != "text" || out[1].Content[1]["type"] != "web_search_tool_result" {
		t.Fatalf("bad assistant blocks: %+v", out[1].Content)
	}
	if out[1].Content[2]["id"] != "call_1" || out[2].Content[0]["tool_use_id"] != "call_1" {
		t.Fatalf("call ids not sanitized: %+v %+v", out[1].Content[2], out[2].Content[0])
	}
	if len(out[2].Content) != 2 { // tool_result merged with the next user text
		t.Fatalf("user messages not merged: %+v", out[2].Content)
	}
}

func TestCacheMarksWithinBudget(t *testing.T) {
	p := New("", "http://x", "k", "m")
	var msgs []agent.Message
	for i := 0; i < 6; i++ {
		role := agent.RoleUser
		if i%2 == 1 {
			role = agent.RoleAssistant
		}
		msgs = append(msgs, agent.Message{Role: role, Parts: []agent.Part{{Kind: agent.PartText, Text: "t"}}})
	}
	msgs = append(msgs, agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "last"}}})
	body := p.buildBody(provider.Request{System: "s", Messages: msgs, CachePoints: []int{1, 2, 3, 4, 5}})
	raw, _ := json.Marshal(body)
	if n := strings.Count(string(raw), `"cache_control"`); n > maxMarks {
		t.Fatalf("%d cache marks, max %d", n, maxMarks)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(529)
			io.WriteString(w, `{"error":{"message":"overloaded"}}`)
			return
		}
		io.WriteString(w, sse(
			`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1}}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`))
	}))
	defer srv.Close()
	p := New("", srv.URL, "k", "m")
	p.Backoff = time.Millisecond
	msg, _ := collect(p, provider.Request{Messages: []agent.Message{{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: "x"}}}}})
	if msg.StopReason != agent.StopEnd || calls.Load() != 2 {
		t.Fatalf("stop=%s calls=%d", msg.StopReason, calls.Load())
	}
}

func TestHTTPErrorBecomesErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"invalid x-api-key"}}`)
	}))
	defer srv.Close()
	msg, _ := collect(New("", srv.URL, "bad", "m"), provider.Request{})
	if msg.StopReason != agent.StopError || !strings.Contains(msg.Parts[0].Text, "invalid x-api-key") {
		t.Fatalf("bad error message: %+v", msg)
	}
}

func TestCancelIsAborted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // the server notices a closed connection once the body is read
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var final agent.Message
	for e := range New("", srv.URL, "k", "m").Stream(ctx, provider.Request{}) {
		if e.Kind == provider.EventDone {
			final = *e.Message
		}
	}
	if final.StopReason != agent.StopAborted {
		t.Fatalf("stop=%s", final.StopReason)
	}
}

func TestRefusalIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sse(
			`{"type":"message_start","message":{"id":"m","usage":{}}}`,
			`{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"explanation":"policy"}},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`))
	}))
	defer srv.Close()
	msg, _ := collect(New("", srv.URL, "k", "m"), provider.Request{})
	if msg.StopReason != agent.StopError || !strings.Contains(msg.Parts[0].Text, "policy") {
		t.Fatalf("bad refusal: %+v", msg)
	}
}
