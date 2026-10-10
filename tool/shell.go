package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Shell runs a command with `bash -c` in Dir.
type Shell struct {
	Dir            string
	DefaultTimeout time.Duration // 0: two minutes
}

func (Shell) Name() string { return "shell" }
func (Shell) Description() string {
	return "Run a shell command (bash -c) in the working directory and return its combined stdout and stderr. " +
		"Output beyond 2000 lines or 50 KB keeps only the end. A non-zero exit is reported as an error."
}
func (Shell) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
"command":{"type":"string","description":"The command line to run."},
"timeout":{"type":"integer","minimum":1,"description":"Seconds before the command is killed. Default 120."}
},"required":["command"],"additionalProperties":false}`)
}

func (s Shell) Execute(ctx context.Context, args json.RawMessage) Result {
	var a struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	timeout := s.DefaultTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	if a.Timeout > 0 {
		timeout = time.Duration(a.Timeout) * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, "bash", "-c", a.Command)
	cmd.Dir = s.Dir
	killGroup(cmd)
	var out liveBuffer // shown as it comes (Update)
	cmd.Stdout, cmd.Stderr = &out, &out
	stop := make(chan struct{})
	go out.report(ctx, 250*time.Millisecond, stop)
	err := cmd.Run()
	close(stop)

	text, cut := truncateTail(out.String())
	if cut {
		text = "[output truncated, showing the end]\n" + text
	}
	switch {
	case ctx.Err() != nil:
		return Result{Text: text + note("cancelled"), IsError: true}
	case errors.Is(tctx.Err(), context.DeadlineExceeded):
		return Result{Text: text + note("timed out after %s", timeout), IsError: true}
	}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return Result{Text: text + note("exit status %d", exit.ExitCode()), IsError: true}
	case err != nil:
		return Result{Text: fmt.Sprintf("%s%s", text, note("%v", err)), IsError: true}
	}
	if text == "" {
		text = "(no output)"
	}
	return Result{Text: text}
}
