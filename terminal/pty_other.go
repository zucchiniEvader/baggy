//go:build !(darwin || linux || freebsd)

package terminal

import "os/exec"

// Supported reports whether commands get terminals of their own here.
const Supported = false

func start(s *Session, cmd *exec.Cmd) error { return ErrUnsupported }
