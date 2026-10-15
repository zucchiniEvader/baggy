//go:build unix

package claudecode

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroup runs cmd in its own process group and kills the whole group
// when the run is stopped, so what Claude Code started goes with it.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
}
