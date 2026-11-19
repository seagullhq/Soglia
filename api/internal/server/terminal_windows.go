//go:build windows

package server

import "os/exec"

// On Windows the default CommandContext cancellation (Process.Kill) is used;
// there is no portable equivalent of a Unix process group here.
func configureTerminalProcess(cmd *exec.Cmd) {}
