package tool

import (
	"strings"
	"testing"
)

const sch = `{"type":"object","properties":{
  "cmd":{"type":"string"},
  "n":{"type":"integer","minimum":1,"maximum":10},
  "on":{"type":"boolean"},
  "mode":{"type":"string","enum":["a","b"]},
  "xs":{"type":"array","items":{"type":"number"}}
},"required":["cmd"],"additionalProperties":false}`

func TestValidateArgs(t *testing.T) {
	ok := []struct{ in, want string }{
		{`{"cmd":"ls"}`, `{"cmd":"ls"}`},
		{``, ""}, // fails: cmd required (checked below)
		{`{"cmd":"ls","n":"3","on":"true"}`, `{"cmd":"ls","n":3,"on":true}`},
		{`{"cmd":"ls","n":null,"mode":null}`, `{"cmd":"ls"}`},
		{`{"cmd":"ls","xs":["1.5",2]}`, `{"cmd":"ls","xs":[1.5,2]}`},
	}
	for _, c := range ok {
		got, err := ValidateArgs("t", []byte(sch), []byte(c.in))
		if c.want == "" {
			if err == nil {
				t.Errorf("%q: want error", c.in)
			}
			continue
		}
		if err != nil || string(got) != c.want {
			t.Errorf("%q: got %s, %v; want %s", c.in, got, err, c.want)
		}
	}

	bad := []struct{ in, msg string }{
		{`{"n":1}`, "cmd: is required"},
		{`{"cmd":1}`, "cmd: expected string, got integer"},
		{`{"cmd":"x","n":11}`, "n: 11 is above the maximum"},
		{`{"cmd":"x","n":1.5}`, "n: expected integer, got number"},
		{`{"cmd":"x","mode":"c"}`, `mode: must be one of ["a","b"]`},
		{`{"cmd":"x","xs":[1,"y"]}`, "xs.1: expected number, got string"},
		{`{"cmd":"x","extra":1}`, "extra: is not allowed"},
		{`[1]`, "root: expected object, got array"},
		{`{bad`, "not valid JSON"},
	}
	for _, c := range bad {
		_, err := ValidateArgs("t", []byte(sch), []byte(c.in))
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%q: got %v; want %q", c.in, err, c.msg)
		}
	}
}

func TestValidateArgsUnreadableSchema(t *testing.T) {
	got, err := ValidateArgs("t", []byte(`{`), []byte(`{"a":1}`))
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("got %s, %v", got, err)
	}
}
