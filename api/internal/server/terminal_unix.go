//go:build !windows

package server

import (
	"os/exec"
	"syscall"
)

// configureTerminalProcess puts a terminal command in its own process group so
// that cancelling it also stops children such as `sleep` or background jobs.
// Without this, CommandContext only kills the shell, and orphaned children keep
// the output pipe open until they exit on their own.
func configureTerminalProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// A negative pid targets the whole process group.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
}
