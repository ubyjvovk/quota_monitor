package kimi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
		Run: func(_ context.Context, _ []string, _ io.Reader) error {
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
		Run:      func(context.Context, []string, io.Reader) error { return nil },
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
		Run: func(_ context.Context, _ []string, stdin io.Reader) error {
			started := time.Now()
			first := make([]byte, len("/exit\r"))
			count, err := stdin.Read(first)
			firstReadAfter = time.Since(started)
			input = append(input, first[:count]...)
			rest, readErr := io.ReadAll(stdin)
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
