// Package codemode runs a model-written JavaScript script that calls tools,
// after pi's codemode: only what the script prints or returns reaches the
// model, so it can page through an API, run calls in parallel, filter what
// came back, and hand over the lines that matter. Each script gets a fresh
// goja VM with no timers, network, file system or modules; its one way out
// is the host's tools.
package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// ToolInfo is a tool a script can call.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"inputSchema,omitempty"`
	ReadOnly    bool            `json:"readOnly"`
}

// Host gives a script its tools and its store.
type Host interface {
	// Call runs a tool; the result is any JSON value (a string for text).
	Call(ctx context.Context, name string, args json.RawMessage) (any, error)
	Tools() []ToolInfo
}

// Store keeps JSON values for later scripts; optional.
type Store interface {
	Load(key string) (json.RawMessage, bool)
	Save(key string, value json.RawMessage) error
}

type Options struct {
	Timeout         time.Duration // 30 minutes when zero
	MaxOutputTokens int           // 10,000 when zero
	MaxCalls        int           // calls not yet awaited, 1000 when zero
	Concurrency     int           // calls at once, 8 when zero
	Store           Store
}

// Result is how a script ended.
type Result struct {
	Output    string // what it printed and returned, cut to the budget
	Truncated bool
	Calls     int
	Value     string // what the script returned, as Output shows it; "" for nothing
	Err       error  // a throw, a timeout, a stop
}

// Empty reports whether a returned value says there is nothing: undefined,
// null, false, or an empty string, list or object.
func (r Result) Empty() bool {
	switch strings.TrimSpace(r.Value) {
	case "", "false", "[]", "{}", `""`:
		return true
	}
	return false
}

// Text is the result as the model reads it.
func (r Result) Text() string {
	s := r.Output
	if r.Err != nil {
		if s != "" {
			s += "\n"
		}
		s += "Error: " + r.Err.Error()
	}
	if s == "" {
		s = "(the script printed and returned nothing)"
	}
	return s
}

// options reads a first line "// @options: {...}".
func options(code string, o Options) Options {
	first, _, _ := strings.Cut(strings.TrimSpace(code), "\n")
	rest, ok := strings.CutPrefix(strings.TrimSpace(first), "// @options:")
	if !ok {
		return o
	}
	var v struct {
		MaxOutputTokens int `json:"max_output_tokens"`
		TimeoutMS       int `json:"timeout_ms"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(rest)), &v) == nil {
		if v.MaxOutputTokens > 0 {
			o.MaxOutputTokens = min(v.MaxOutputTokens, 50000)
		}
		if v.TimeoutMS > 0 && (o.Timeout == 0 || time.Duration(v.TimeoutMS)*time.Millisecond < o.Timeout) {
			o.Timeout = time.Duration(v.TimeoutMS) * time.Millisecond
		}
	}
	return o
}

const prelude = `
globalThis.tools = new Proxy({}, { get: (_, name) => typeof name === "string" ? (args) => __call(name, args === undefined ? {} : args) : undefined });
globalThis.console = { log: (...a) => __print(a), info: (...a) => __print(a), warn: (...a) => __print(a), error: (...a) => __print(a) };
globalThis.print = (...a) => __print(a);
`

// Run runs code, the body of an async function: top-level await and return
// work.
func Run(ctx context.Context, code string, host Host, o Options) Result {
	o = options(code, o)
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Minute
	}
	if o.MaxOutputTokens <= 0 {
		o.MaxOutputTokens = 10000
	}
	if o.MaxCalls <= 0 {
		o.MaxCalls = 1000
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 8
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	var out strings.Builder
	settled := make(chan func(), o.MaxCalls) // resolutions, run on this goroutine
	sem := make(chan struct{}, o.Concurrency)
	var wg sync.WaitGroup
	pending, calls := 0, 0
	tools := map[string]ToolInfo{}
	for _, t := range host.Tools() {
		tools[t.Name] = t
	}
	defer func() { cancel(); wg.Wait() }() // calls still out end with the script

	throw := func(msg string) { panic(vm.NewGoError(errors.New(msg))) }
	toJS := func(v any) goja.Value {
		b, err := json.Marshal(v)
		if err != nil {
			return vm.ToValue(fmt.Sprint(v))
		}
		var x any
		json.Unmarshal(b, &x)
		return vm.ToValue(x)
	}
	vm.Set("__print", func(args []goja.Value) {
		for i, a := range args {
			if i > 0 {
				out.WriteByte(' ')
			}
			out.WriteString(show(vm, a))
		}
		out.WriteByte('\n')
	})
	vm.Set("__call", func(name string, args goja.Value) *goja.Promise {
		p, resolve, reject := vm.NewPromise()
		if _, ok := tools[name]; !ok {
			reject(vm.NewGoError(fmt.Errorf("no tool %q%s", name, suggest(name, tools))))
			return p
		}
		if pending >= o.MaxCalls {
			throw(fmt.Sprintf("more than %d calls not yet awaited", o.MaxCalls))
		}
		raw, err := json.Marshal(args.Export())
		if err != nil {
			reject(vm.NewGoError(fmt.Errorf("the arguments of %s do not read as JSON: %v", name, err)))
			return p
		}
		pending++
		calls++
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				settled <- func() { reject(vm.NewGoError(ctx.Err())) }
				return
			}
			v, err := host.Call(ctx, name, raw)
			<-sem
			settled <- func() {
				if err != nil {
					reject(vm.NewGoError(err))
				} else {
					resolve(toJS(v))
				}
			}
		}()
		return p
	})
	vm.Set("searchTools", func(q string) goja.Value { return toJS(search(tools, q)) })
	vm.Set("describeTool", func(name string) goja.Value {
		t, ok := tools[name]
		if !ok {
			throw(fmt.Sprintf("no tool %q%s", name, suggest(name, tools)))
		}
		return toJS(t)
	})
	vm.Set("store", func(key string, v goja.Value) {
		if o.Store == nil {
			throw("store() is not available here")
		}
		b, err := json.Marshal(v.Export())
		if err != nil || len(b) > 1<<20 {
			throw("store() takes a JSON value of at most 1 MB")
		}
		if err := o.Store.Save(key, b); err != nil {
			throw(err.Error())
		}
	})
	vm.Set("load", func(key string) goja.Value {
		if o.Store == nil {
			throw("load() is not available here")
		}
		b, ok := o.Store.Load(key)
		if !ok {
			return goja.Undefined()
		}
		var x any
		json.Unmarshal(b, &x)
		return vm.ToValue(x)
	})

	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
	defer stop()
	res := Result{}
	finish := func(err error) Result {
		res.Calls = calls
		res.Output, res.Truncated = budget(out.String(), o.MaxOutputTokens*4)
		if err != nil {
			res.Err = cleanErr(err, ctx)
		}
		return res
	}
	if _, err := vm.RunString(prelude); err != nil {
		return finish(err)
	}
	v, err := vm.RunString("(async () => {\n" + code + "\n})()")
	if err != nil {
		return finish(err)
	}
	p, _ := v.Export().(*goja.Promise)
	for p != nil && p.State() == goja.PromiseStatePending {
		if pending == 0 {
			return finish(errors.New("the script waits on something that never comes"))
		}
		select {
		case fn := <-settled:
			pending--
			if err := run(fn); err != nil {
				return finish(err)
			}
		case <-ctx.Done():
			return finish(ctx.Err())
		}
	}
	if p == nil {
		return finish(nil)
	}
	if p.State() == goja.PromiseStateRejected {
		return finish(errors.New(show(vm, p.Result())))
	}
	if r := p.Result(); r != nil && !goja.IsUndefined(r) && !goja.IsNull(r) {
		res.Value = show(vm, r)
		out.WriteString(res.Value)
	}
	return finish(nil)
}

// run calls a resolution, turning an interrupt into an error.
func run(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if ie, ok := r.(*goja.InterruptedError); ok {
				err = ie
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

func cleanErr(err error, ctx context.Context) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return errors.New("the script ran out of time")
	case errors.Is(ctx.Err(), context.Canceled):
		return errors.New("stopped")
	}
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return errors.New(ex.Value().String())
	}
	return err
}

// show is a value as text: a string as it is, anything else as JSON.
func show(vm *goja.Runtime, v goja.Value) string {
	if v == nil || goja.IsUndefined(v) {
		return "undefined"
	}
	if s, ok := v.Export().(string); ok {
		return s
	}
	if obj, ok := v.(*goja.Object); ok {
		if msg := obj.Get("message"); msg != nil && obj.Get("stack") != nil {
			return msg.String()
		}
	}
	b, err := json.MarshalIndent(v.Export(), "", "  ")
	if err != nil {
		return v.String()
	}
	return string(b)
}

// budget keeps the start and the end of s within n bytes.
func budget(s string, n int) (string, bool) {
	s = strings.TrimRight(s, "\n")
	if len(s) <= n {
		return s, false
	}
	head, tail := s[:n*2/3], s[len(s)-n/3:]
	return fmt.Sprintf("%s\n[… %d characters cut …]\n%s", head, len(s)-len(head)-len(tail), tail), true
}

func search(tools map[string]ToolInfo, q string) []ToolInfo {
	words := strings.Fields(strings.ToLower(q))
	type scored struct {
		t ToolInfo
		n int
	}
	var hits []scored
	for _, t := range tools {
		text := strings.ToLower(t.Name + " " + t.Description)
		n := 0
		for _, w := range words {
			if strings.Contains(text, w) {
				n++
			}
		}
		if n > 0 || len(words) == 0 {
			t.Schema = nil
			hits = append(hits, scored{t, n})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].n != hits[j].n {
			return hits[i].n > hits[j].n
		}
		return hits[i].t.Name < hits[j].t.Name
	})
	var out []ToolInfo
	for i, h := range hits {
		if i == 20 {
			break
		}
		out = append(out, h.t)
	}
	return out
}

// suggest names the closest tools to a name no tool has.
func suggest(name string, tools map[string]ToolInfo) string {
	norm := func(s string) string { return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s)) }
	var close []string
	for n := range tools {
		if norm(n) == norm(name) || strings.Contains(norm(n), norm(name)) {
			close = append(close, n)
		}
	}
	sort.Strings(close)
	if len(close) == 0 && len(tools) <= 12 {
		for n := range tools {
			close = append(close, n)
		}
		sort.Strings(close)
	}
	if len(close) == 0 {
		return "; searchTools(query) finds tools"
	}
	if len(close) > 5 {
		close = close[:5]
	}
	return "; did you mean " + strings.Join(close, ", ") + "?"
}
