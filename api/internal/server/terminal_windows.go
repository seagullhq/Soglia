//go:build windows

package server

import (
	"errors"
	"os/exec"
)

// The Linux container is the only supported target for the diagnostic
// terminal. Refusing here avoids cmd.exe, which cannot be safely restricted
// to read-only commands on Windows.
func trustedProgramPath(name string) (string, error) {
	return "", errors.New("the diagnostic terminal is only available in the Linux container")
}

func terminalEnv(cwd string) []string { return nil }

func configureTerminalProcess(cmd *exec.Cmd) {}
