//go:build unix

package tool

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroup runs cmd in its own process group and kills the whole group on
// cancel, so a command's children do not outlive it or hold its pipes open.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
}
