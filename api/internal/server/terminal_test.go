package server

import (
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestSplitTerminalCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
		wantErr string
	}{
		{name: "simple", command: "ls -la /app", want: []string{"ls", "-la", "/app"}},
		{name: "quoted argument", command: `find . -name "*.log"`, want: []string{"find", ".", "-name", "*.log"}},
		{name: "semicolon", command: "cat /app/settings.json; rm -rf /", wantErr: "shell operator"},
		{name: "pipe", command: "ps aux | grep python", wantErr: "shell operator"},
		{name: "redirection", command: "echo hi > /app/x", wantErr: "shell operator"},
		{name: "substitution", command: "echo $(whoami)", wantErr: "shell operator"},
		{name: "newline", command: "echo a\necho b", wantErr: "newlines"},
		{name: "unterminated quote", command: `cat "foo`, wantErr: "unterminated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := splitTerminalCommand(test.command)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestTerminalValidateArgs(t *testing.T) {
	tests := []struct {
		name    string
		program string
		args    []string
		wantErr bool
	}{
		{name: "safe ls", program: "ls", args: []string{"-la", "/app"}},
		{name: "safe find", program: "find", args: []string{"/app", "-name", "*.log"}},
		{name: "find exec", program: "find", args: []string{"/app", "-exec", "rm", "{}", ";"}, wantErr: true},
		{name: "find delete", program: "find", args: []string{"/app", "-delete"}, wantErr: true},
		{name: "find fprint", program: "find", args: []string{"/app", "-fprintf", "out"}, wantErr: true},
		{name: "tail follow", program: "tail", args: []string{"-f", "log"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := terminalValidateArgs(test.program, test.args)
			if test.wantErr && err == nil {
				t.Fatal("expected the arguments to be rejected")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTerminalPathArgs(t *testing.T) {
	got := terminalPathArgs([]string{"-la", "--file=/etc/shadow", "/app", "pattern"})
	want := []string{"/etc/shadow", "/app", "pattern"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRedactTerminalSecrets(t *testing.T) {
	got := redactTerminalSecrets("password=supersecret123, again supersecret123", []string{"supersecret123"})
	want := "password=********, again ********"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveTerminalPathConfinesToApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the files root is a Linux container path")
	}
	got, err := resolveTerminalPath(filesRoot, "v6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/app/v6" {
		t.Fatalf("got %q, want /app/v6", got)
	}
	for _, raw := range []string{"/etc/passwd", "../etc", "/app/../../etc"} {
		if _, err := resolveTerminalPath(filesRoot, raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}
