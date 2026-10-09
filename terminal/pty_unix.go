//go:build darwin || linux || freebsd

package terminal

import (
	"errors"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// Supported reports whether commands get terminals of their own here.
const Supported = true

// start runs cmd on a new 80×24 terminal, leading its own session and
// process group, which is what stopping it kills.
func start(s *Session, cmd *exec.Cmd) error {
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		return err
	}
	s.write = func(b []byte) error { _, err := ptmx.Write(b); return err }
	pgid := cmd.Process.Pid
	s.stop = func() { syscall.Kill(-pgid, syscall.SIGKILL) }
	read := make(chan struct{})
	go func() { // drain it whether anyone reads or not, so the command never blocks
		defer close(read)
		buf := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				s.output(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code, err = ee.ExitCode(), nil
			if code < 0 {
				code = 1
			}
		}
		ptmx.Close()
		<-read
		s.exited(code, err)
	}()
	return nil
}
