//go:build !windows

package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// terminalPrograms is the complete allow-list. Every entry is a read-only
// diagnostic utility; no interpreter, package manager, editor, network client
// or shell can be started from the terminal.
var terminalPrograms = map[string]bool{
	"cat": true, "cut": true, "date": true, "df": true, "du": true,
	"echo": true, "find": true, "free": true, "grep": true, "head": true,
	"hostname": true, "id": true, "ls": true, "nl": true, "nproc": true,
	"ps": true, "pwd": true, "sort": true, "stat": true, "tail": true,
	"tr": true, "uname": true, "uniq": true, "uptime": true, "wc": true,
	"whoami": true,
}

var terminalTrustedDirs = []string{"/usr/bin", "/bin"}

func trustedProgramPath(name string) (string, error) {
	if !terminalPrograms[name] {
		return "", fmt.Errorf("command %q is not in the read-only allow-list", name)
	}
	for _, dir := range terminalTrustedDirs {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		// A world- or group-writable binary could be replaced with arbitrary
		// code, turning an allow-listed command into a shell by another name.
		if info.Mode().Perm()&0o022 != 0 {
			return "", fmt.Errorf("command %q is writable by other users", name)
		}
		return candidate, nil
	}
	return "", fmt.Errorf("command %q is not available", name)
}

// terminalEnv replaces the inherited environment so commands cannot print
// database passwords or other secrets, and PATH cannot resolve a planted
// binary ahead of the trusted system directories.
func terminalEnv(cwd string) []string {
	return []string{
		"PATH=/usr/bin:/bin",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"TERM=dumb",
		"HOME=" + cwd,
	}
}

// configureTerminalProcess puts a command in its own process group so
// cancellation also stops children.
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
