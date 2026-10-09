// Package shellenv reads the environment the user's login shell sets up,
// since an app started from the Dock or Finder inherits launchd's bare PATH
// and would not find npx, uvx, brew's tools or a version manager's.
package shellenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const marker = "\x00__gobot_env__\x00"

var (
	once sync.Once
	env  []string
)

// Env is the login shell's environment, read once (ten seconds at most);
// the process's own when it cannot be read, and always on Windows.
func Env() []string {
	once.Do(func() {
		env = read()
		if env == nil {
			env = os.Environ()
		}
	})
	return append([]string(nil), env...)
}

func read() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Interactive and login, so .zshrc and .bash_profile both run; what they
	// print before the marker is dropped.
	cmd := exec.CommandContext(ctx, sh, "-l", "-i", "-c", `printf '\0__gobot_env__\0'; env -0`)
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	i := bytes.Index(out, []byte(marker))
	if i < 0 {
		return nil
	}
	var vars []string
	for _, kv := range strings.Split(string(out[i+len(marker):]), "\x00") {
		if k, _, ok := strings.Cut(kv, "="); ok && k != "" && !strings.HasPrefix(k, "_") && k != "SHLVL" && k != "PWD" && k != "OLDPWD" {
			vars = append(vars, kv)
		}
	}
	if len(vars) == 0 {
		return nil
	}
	return vars
}

// With is env with the variables in add set over it.
func With(env []string, add ...string) []string {
	out := make([]string, 0, len(env)+len(add))
	set := map[string]bool{}
	for _, kv := range add {
		k, _, _ := strings.Cut(kv, "=")
		set[k] = true
	}
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); !set[k] {
			out = append(out, kv)
		}
	}
	return append(out, add...)
}
