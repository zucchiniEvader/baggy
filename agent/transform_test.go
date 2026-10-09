package agent

import (
	"reflect"
	"strings"
	"testing"
)

var opts = TransformOptions{Provider: "p", Model: "m", SupportsImages: true}

func asst(provider string, stop StopReason, parts ...Part) Message {
	return Message{Role: RoleAssistant, Provider: provider, Model: "m", StopReason: stop, Parts: parts}
}
func text(s string) Part    { return Part{Kind: PartText, Text: s} }
func user(s string) Message { return Message{Role: RoleUser, Parts: []Part{text(s)}} }
func call(id string) Part   { return Part{Kind: PartToolCall, CallID: id, Name: "read"} }
func result(id, s string) Message {
	return Message{Role: RoleTool, Parts: []Part{{Kind: PartToolResult, CallID: id, Text: s}}}
}

func TestTransformThinkingAndServerBlocks(t *testing.T) {
	own := asst("p", StopEnd,
		Part{Kind: PartThinking, Signature: "sig"},
		Part{Kind: PartThinking, Text: "unsigned"},
		Part{Kind: PartServer})
	foreign := asst("other", StopEnd,
		Part{Kind: PartThinking, Text: "old reasoning", Signature: "x"},
		Part{Kind: PartThinking, Text: "  "},
		Part{Kind: PartServer},
		text("hi"))
	in := []Message{foreign, own}
	out, _ := Transform(in, opts)

	if want := []Part{text("old reasoning"), text("hi")}; !reflect.DeepEqual(out[0].Parts, want) {
		t.Errorf("foreign: %+v", out[0].Parts)
	}
	if len(out[1].Parts) != 3 {
		t.Errorf("own: %+v", out[1].Parts)
	}
	if len(in[0].Parts) != 4 || in[0].Parts[0].Kind != PartThinking {
		t.Errorf("input modified: %+v", in[0].Parts)
	}
}

func TestTransformDropsFailedTurnsAndFillsResults(t *testing.T) {
	in := []Message{
		user("go"),
		asst("p", StopError, text("partial")),
		asst("p", StopToolUse, call("a"), call("b")),
		result("a", "ok"),
		user("next"),
		asst("p", StopToolUse, call("c")),
	}
	out, origins := Transform(in, opts)

	var roles []Role
	for _, m := range out {
		roles = append(roles, m.Role)
	}
	if want := []Role{RoleUser, RoleAssistant, RoleTool, RoleTool, RoleUser, RoleAssistant, RoleTool}; !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles %v", roles)
	}
	if want := []int{0, 2, 3, 3, 4, 5, 5}; !reflect.DeepEqual(origins, want) {
		t.Errorf("origins %v", origins)
	}
	b := out[3].Parts[0]
	if b.CallID != "b" || !b.IsError || b.Text != NoResultProvided {
		t.Errorf("made-up result: %+v", b)
	}
	if c := out[6].Parts[0]; c.CallID != "c" || !c.IsError {
		t.Errorf("trailing result: %+v", c)
	}
}

func TestTransformNormalizesForeignToolCallIDs(t *testing.T) {
	o := opts
	o.NormalizeToolCallID = func(id string) string { return strings.ReplaceAll(id, "|", "_") }
	in := []Message{asst("other", StopToolUse, call("call|weird")), result("call|weird", "ok")}
	out, _ := Transform(in, o)
	if out[0].Parts[0].CallID != "call_weird" || out[1].Parts[0].CallID != "call_weird" {
		t.Fatalf("%+v", out)
	}
	if in[0].Parts[0].CallID != "call|weird" || in[1].Parts[0].CallID != "call|weird" {
		t.Fatalf("input modified")
	}
	own, _ := Transform([]Message{asst("p", StopToolUse, call("call|weird")), result("call|weird", "ok")}, o)
	if own[0].Parts[0].CallID != "call|weird" {
		t.Fatalf("own id rewritten: %+v", own)
	}
}

func TestTransformImagesForTextOnlyModel(t *testing.T) {
	img := Part{Kind: PartImage, MediaType: "image/png", Data: []byte("a")}
	in := []Message{{Role: RoleUser, Parts: []Part{text("see"), img, img}}}
	o := opts
	o.SupportsImages = false
	out, _ := Transform(in, o)
	if want := []Part{text("see"), text(NonVisionUserImage)}; !reflect.DeepEqual(out[0].Parts, want) {
		t.Fatalf("%+v", out[0].Parts)
	}
	if same, _ := Transform(in, opts); len(same[0].Parts) != 3 {
		t.Fatalf("vision model lost images: %+v", same[0].Parts)
	}
}

func TestPrefixLen(t *testing.T) {
	origins := []int{0, 2, 3, 3, 4}
	for n, want := range map[int]int{0: 0, 1: 1, 2: 1, 3: 2, 4: 4, 5: 5} {
		if got := PrefixLen(origins, n); got != want {
			t.Errorf("PrefixLen(%d) = %d, want %d", n, got, want)
		}
	}
}
