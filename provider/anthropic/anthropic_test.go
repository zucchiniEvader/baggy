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

func TestBuildBodyTransformsTranscript(t *testing.T) {
	p := New("anthropic", "", "k", "m")
	user := func(s string) agent.Message {
		return agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
	}
	failed := agent.Message{Role: agent.RoleAssistant, Provider: "anthropic", Model: "m", StopReason: agent.StopError,
		Parts: []agent.Part{{Kind: agent.PartText, Text: "boom", IsError: true}}}
	foreign := agent.Message{Role: agent.RoleAssistant, Provider: "other", Model: "x", StopReason: agent.StopEnd,
		Parts: []agent.Part{{Kind: agent.PartThinking, Text: "reasoned", Signature: "alien"}, {Kind: agent.PartText, Text: "ok"}}}
	body := p.buildBody(provider.Request{
		Messages:    []agent.Message{user("a"), failed, user("b"), foreign, user("c")},
		CachePoints: []int{4}, // through the foreign reply, which sits at index 2 once the failed turn is gone
	})
	b, _ := json.Marshal(body["messages"])
	s := string(b)
	if strings.Contains(s, "boom") || strings.Contains(s, "alien") || strings.Contains(s, `"thinking"`) || !strings.Contains(s, "reasoned") {
		t.Fatalf("transcript not transformed: %s", s)
	}
	var msgs []struct {
		Role    string
		Content []map[string]any
	}
	json.Unmarshal(b, &msgs)
	if len(msgs) != 3 || msgs[2].Content[0]["cache_control"] == nil || msgs[1].Content[len(msgs[1].Content)-1]["cache_control"] == nil {
		t.Fatalf("cache marks: %s", s)
	}
}

func TestDropBoundThinking(t *testing.T) {
	req := provider.Request{System: "you are A"}
	key := contextKey(req)
	think := func(s, k string) agent.Message {
		return agent.Message{Role: agent.RoleAssistant, Provider: "anthropic", Model: "claude-sonnet-5-5", StopReason: agent.StopToolUse,
			Parts: []agent.Part{{Kind: agent.PartThinking, Text: s, Signature: "sig-" + s, Raw: thinkingRaw(k)}, {Kind: agent.PartToolCall, CallID: s, Name: "x", Args: json.RawMessage(`{}`)}}}
	}
	result := func(id string) agent.Message {
		return agent.Message{Role: agent.RoleTool, Parts: []agent.Part{{Kind: agent.PartToolResult, CallID: id, Text: "r"}}}
	}
	user := func(s string) agent.Message {
		return agent.Message{Role: agent.RoleUser, Parts: []agent.Part{{Kind: agent.PartText, Text: s}}}
	}
	same := []agent.Message{user("a"), think("one", key), result("one"), user("b"), think("two", key), result("two")}
	if out := DropBoundThinking("claude-sonnet-5-5", key, same); len(out[1].Parts) != 2 || len(out[4].Parts) != 2 {
		t.Fatal("thinking under the same prompt was dropped")
	}
	changed := []agent.Message{user("a"), think("one", "other"), result("one"), user("b"), think("two", key), result("two")}
	out := DropBoundThinking("claude-sonnet-5-5-20260901", key, changed)
	if len(out[1].Parts) != 1 || len(out[4].Parts) != 1 || changed[4].Parts[0].Kind != agent.PartThinking {
		t.Fatalf("stale: %+v / %+v", out[1].Parts, out[4].Parts)
	}
	if kept := DropBoundThinking("claude-haiku-4-5", key, changed); len(kept[1].Parts) != 2 {
		t.Fatal("an unbound model lost its thinking")
	}
	req.Messages = changed
	b, _ := json.Marshal(New("anthropic", "", "k", "claude-sonnet-5-5").buildBody(req)["messages"])
	if strings.Contains(string(b), "sig-") {
		t.Fatalf("request: %s", b)
	}
	req.Messages = same
	b, _ = json.Marshal(New("anthropic", "", "k", "claude-sonnet-5-5").buildBody(req)["messages"])
	if !strings.Contains(string(b), "sig-one") || strings.Contains(string(b), `"ctx"`) {
		t.Fatalf("request: %s", b)
	}
}

func TestSearchStreamsServerToolEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		io.WriteString(w, sse(
			`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"s1","name":"web_search","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"kyoto weather\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"s1","content":[{"type":"web_search_result","url":"https://a"},{"type":"web_search_result","url":"https://b"}]}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"server_tool_use","id":"f1","name":"web_fetch","input":{}}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"url\":\"https://a\"}"}}`,
			`{"type":"content_block_stop","index":2}`,
			`{"type":"content_block_start","index":3,"content_block":{"type":"web_fetch_tool_result","tool_use_id":"f1","content":{"type":"web_fetch_tool_error","error_code":"url_not_accessible"}}}`,
			`{"type":"content_block_stop","index":3}`,
			`{"type":"message_delta","delta":{"stop_reason":"pause_turn"},"usage":{"output_tokens":5}}`,
			`{"type":"message_stop"}`))
	}))
	defer srv.Close()
	p := New("", srv.URL, "k", "claude-sonnet-5-5")
	p.ServerTools = WebTools(srv.URL, "claude-sonnet-5-5")
	msg, evs := collect(p, provider.Request{})
	var blocks []provider.ServerBlock
	for _, e := range evs {
		if e.Kind == provider.EventServerTool {
			b, ok := provider.DescribeServer(*e.Part)
			if !ok {
				t.Fatalf("not a server block: %s", e.Part.Raw)
			}
			blocks = append(blocks, b)
		}
	}
	if len(blocks) != 4 || blocks[0].Detail != "kyoto weather" || !blocks[1].Result || blocks[1].Count != 2 || blocks[1].ID != "s1" ||
		blocks[2].Tool != provider.WebFetch || blocks[2].Detail != "https://a" || blocks[3].Error != "url not accessible" {
		t.Fatalf("blocks %+v", blocks)
	}
	if msg.StopReason != agent.StopPause || len(msg.Parts) != 4 {
		t.Fatalf("message %+v", msg)
	}
}

func TestWebTools(t *testing.T) {
	if ds := WebTools("https://api.deepseek.com/anthropic", "deepseek-chat"); len(ds) != 1 || !strings.Contains(string(ds[0]), "20250305") {
		t.Fatalf("deepseek %s", ds)
	}
	if a := WebTools("", "claude-opus-5-5"); len(a) != 2 || !strings.Contains(string(a[0]), "20260209") {
		t.Fatalf("anthropic %s", a)
	}
	if h := WebTools("", "claude-haiku-4-5-20251001"); !strings.Contains(string(h[1]), "web_fetch_20250910") {
		t.Fatalf("haiku %s", h)
	}
	if o := WebTools("", "claude-opus-4-8"); !strings.Contains(string(o[0]), "20260209") {
		t.Fatalf("opus 4.8 %s", o)
	}
}
