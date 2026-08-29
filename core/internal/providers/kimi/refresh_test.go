package kimi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"quotamon/internal/source"
)

func TestCLIRefresherSucceedsWhenTheCLIWritesAFutureExpiry(t *testing.T) {
	path := staleCredentialFile(t)
	refresher := CLIRefresher{
		Binary:   "kimi",
		Startup:  time.Nanosecond,
		Deadline: time.Second,
		Store:    CredentialStore{Path: path},
		Run: func(*exec.Cmd) error {
			if err := os.WriteFile(path, credentialJSON(time.Now().Add(time.Hour)), 0o600); err != nil {
				return err
			}
			return errors.New("script was killed at its deadline")
		},
	}

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
}

func TestCLIRefresherFailsWhenTheCredentialStaysStale(t *testing.T) {
	refresher := CLIRefresher{
		Binary:   "kimi",
		Startup:  time.Nanosecond,
		Deadline: time.Second,
		Store:    CredentialStore{Path: staleCredentialFile(t)},
		Run:      func(*exec.Cmd) error { return nil },
	}

	err := refresher.Refresh(context.Background())
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "kimi") {
		t.Fatalf("Refresh() error = %v, want a Kimi action", err)
	}
}

func TestCLIRefresherReportsNotConfiguredWhenKimiIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := (CLIRefresher{}).Refresh(context.Background())
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != source.NotConfigured {
		t.Fatalf("Refresh() error = %v, want NotConfigured", err)
	}
}

func TestCLIRefresherSendsExitAfterTheStartupDelay(t *testing.T) {
	path := staleCredentialFile(t)
	startup := 10 * time.Millisecond
	var input []byte
	var firstReadAfter time.Duration
	refresher := CLIRefresher{
		Binary:   "kimi",
		Startup:  startup,
		Deadline: 3 * time.Second,
		Store:    CredentialStore{Path: path},
		Run: func(cmd *exec.Cmd) error {
			started := time.Now()
			first := make([]byte, len("/exit\r"))
			count, err := cmd.Stdin.Read(first)
			firstReadAfter = time.Since(started)
			input = append(input, first[:count]...)
			rest, readErr := io.ReadAll(cmd.Stdin)
			input = append(input, rest...)
			if writeErr := os.WriteFile(path, credentialJSON(time.Now().Add(time.Hour)), 0o600); writeErr != nil {
				return writeErr
			}
			if err != nil {
				return err
			}
			return readErr
		},
	}

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if string(input) != "/exit\r" {
		t.Fatalf("stdin = %q, want exact /exit\\r bytes", input)
	}
	if firstReadAfter < startup {
		t.Fatalf("stdin arrived after %s, before startup delay %s", firstReadAfter, startup)
	}
}

func TestCLIRefresherRunsFromHomeAndSetsTERMWhenUnset(t *testing.T) {
	original, had := os.LookupEnv("TERM")
	if err := os.Unsetenv("TERM"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("TERM", original)
		}
	})

	path := staleCredentialFile(t)
	var command *exec.Cmd
	refresher := CLIRefresher{
		Binary:   "kimi",
		Startup:  time.Nanosecond,
		Deadline: time.Second,
		Store:    CredentialStore{Path: path},
		Run: func(cmd *exec.Cmd) error {
			command = cmd
			return os.WriteFile(path, credentialJSON(time.Now().Add(time.Hour)), 0o600)
		},
	}

	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if command.Dir != home {
		t.Fatalf("command.Dir = %q, want the home directory %q (the TUI refuses untrusted folders)", command.Dir, home)
	}
	term := ""
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, "TERM=") {
			term = strings.TrimPrefix(entry, "TERM=")
		}
	}
	if term != "xterm-256color" {
		t.Fatalf("command.Env TERM = %q, want xterm-256color when unset", term)
	}
}

func staleCredentialFile(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/kimi-code.json"
	if err := os.WriteFile(path, credentialJSON(time.Now().Add(-time.Hour)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func credentialJSON(expiresAt time.Time) []byte {
	return []byte(fmt.Sprintf(`{"access_token":"token","expires_at":%d}`, expiresAt.Unix()))
}
