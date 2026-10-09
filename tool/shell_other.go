//go:build !unix

package tool

import (
	"os/exec"
	"time"
)

func killGroup(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
