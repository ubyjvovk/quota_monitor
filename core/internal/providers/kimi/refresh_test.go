package kimi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"quotamon/internal/cache"
	"quotamon/internal/source"
)

func TestCLIRefresherSucceedsWhenTheCLIWritesAFutureExpiry(t *testing.T) {
	path := staleCredentialFile(t)
	refresher := CLIRefresher{
		Binary:   "kimi",
		Startup:  time.Nanosecond,
		Deadline: time.Second,
		Store:    CredentialStore{Path: path},
		Cache:    cache.Store{Dir: t.TempDir()},
		Command:  writesFutureCredential(t, path),
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
		Cache:    cache.Store{Dir: t.TempDir()},
		Command:  shellCommand("exit 0"),
	}

	err := refresher.Refresh(context.Background())
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "kimi") {
		t.Fatalf("Refresh() error = %v, want a Kimi action", err)
	}
}

func TestCLIRefresherReportsNotConfiguredWhenKimiIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := (CLIRefresher{Cache: cache.Store{Dir: t.TempDir()}}).Refresh(context.Background())
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != source.NotConfigured {
		t.Fatalf("Refresh() error = %v, want NotConfigured", err)
	}
}

func TestCLIRefresherSendsExitAfterTheStartupDelay(t *testing.T) {
	path := staleCredentialFile(t)
	startup := 10 * time.Millisecond
	inputPath := filepath.Join(t.TempDir(), "input")
	t.Setenv("KIMI_TEST_INPUT", inputPath)
	t.Setenv("KIMI_TEST_CREDENTIAL", path)
	t.Setenv("KIMI_TEST_CREDENTIAL_JSON", string(credentialJSON(time.Now().Add(time.Hour))))
	refresher := CLIRefresher{
		Binary:   "kimi",
		Startup:  startup,
		Deadline: 3 * time.Second,
		Store:    CredentialStore{Path: path},
		Cache:    cache.Store{Dir: t.TempDir()},
		Command: shellCommand(
			`cat > "$KIMI_TEST_INPUT"; printf '%s' "$KIMI_TEST_CREDENTIAL_JSON" > "$KIMI_TEST_CREDENTIAL"`,
		),
	}

	started := time.Now()
	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(input) != "/exit\r" {
		t.Fatalf("stdin = %q, want exact /exit\\r bytes", input)
	}
	if elapsed := time.Since(started); elapsed < startup {
		t.Fatalf("Refresh() returned after %s, before startup delay %s", elapsed, startup)
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
		Cache:    cache.Store{Dir: t.TempDir()},
		Command: func(name string, arg ...string) *exec.Cmd {
			command = writesFutureCredential(t, path)(name, arg...)
			return command
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

func TestCLIRefresherKillsTheWholeProcessGroupAtItsDeadline(t *testing.T) {
	var command *exec.Cmd
	refresher := CLIRefresher{
		Binary:   "sh",
		Startup:  time.Nanosecond,
		Deadline: time.Second,
		Store:    CredentialStore{Path: staleCredentialFile(t)},
		Cache:    cache.Store{Dir: t.TempDir()},
		Command: func(string, ...string) *exec.Cmd {
			command = exec.Command("sh", "-c", "sleep 60 & sleep 60")
			return command
		},
	}

	started := time.Now()
	err := refresher.Refresh(context.Background())
	if err == nil {
		t.Fatal("Refresh() succeeded with stale credentials")
	}
	if elapsed := time.Since(started); elapsed >= 4*time.Second {
		t.Fatalf("Refresh() took %s, want less than four seconds", elapsed)
	}
	if command == nil || command.Process == nil {
		t.Fatal("Refresh() did not start the test process")
	}
	if err := syscall.Kill(-command.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kill(-%d, 0) = %v, want ESRCH after group cleanup", command.Process.Pid, err)
	}
}

func TestCLIRefresherReturnsImmediatelyWhenTheRefreshLockIsHeld(t *testing.T) {
	directory := t.TempDir()
	lock, err := os.OpenFile(filepath.Join(directory, refreshLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	spawned := filepath.Join(directory, "spawned")
	t.Setenv("KIMI_TEST_SPAWNED", spawned)
	refresher := CLIRefresher{
		Binary:  "sh",
		Store:   CredentialStore{Path: staleCredentialFile(t)},
		Cache:   cache.Store{Dir: directory},
		Command: shellCommand(`touch "$KIMI_TEST_SPAWNED"`),
	}

	err = refresher.Refresh(context.Background())
	assertSourceError(t, err, source.Transport, "Kimi refresh already in progress")
	if _, statErr := os.Stat(spawned); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("spawn marker stat error = %v, want not exist", statErr)
	}
}

func TestCLIRefresherSweepsOnlyARecordedMatchingProcessGroup(t *testing.T) {
	binary, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		processArgs []string
		wantKilled  bool
	}{
		{
			name:        "matching script and Kimi path is killed",
			processArgs: []string{"sh", "-c", "sleep 60 & wait", "script", binary},
			wantKilled:  true,
		},
		{
			name:        "unrelated recorded group is left alone",
			processArgs: []string{"sh", "-c", "sleep 60 & wait", "unrelated"},
			wantKilled:  false,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stale := exec.Command(test.processArgs[0], test.processArgs[1:]...)
			stale.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := stale.Start(); err != nil {
				t.Fatal(err)
			}
			pgid := stale.Process.Pid
			t.Cleanup(func() {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				_ = stale.Wait()
			})

			directory := t.TempDir()
			record := refreshRecord{
				PGID:    pgid,
				Started: time.Now().Add(-refreshRateCap).Add(-time.Minute).UTC().Truncate(time.Second).Format(time.RFC3339),
			}
			if err := writeRefreshRecord(filepath.Join(directory, refreshPIDName), record); err != nil {
				t.Fatal(err)
			}
			aliveAtLaunch := false
			refresher := CLIRefresher{
				Binary:   binary,
				Startup:  time.Nanosecond,
				Deadline: time.Second,
				Store:    CredentialStore{Path: staleCredentialFile(t)},
				Cache:    cache.Store{Dir: directory},
				GroupCommand: func(int) (string, error) {
					if test.wantKilled {
						return "script " + binary, nil
					}
					return "unrelated command", nil
				},
				Command: func(string, ...string) *exec.Cmd {
					if test.wantKilled {
						_ = stale.Wait()
					}
					aliveAtLaunch = syscall.Kill(-pgid, 0) == nil
					return exec.Command("sh", "-c", "exit 0")
				},
			}

			_ = refresher.Refresh(context.Background())
			if aliveAtLaunch == test.wantKilled {
				t.Fatalf("recorded group alive at launch = %v, want %v", aliveAtLaunch, !test.wantKilled)
			}
		})
	}
}

func TestCLIRefresherRateCapsARecentAttemptWithoutLaunching(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	record := refreshRecord{
		PGID:    999999,
		Started: now.Add(-2 * time.Minute).Format(time.RFC3339),
	}
	if err := writeRefreshRecord(filepath.Join(directory, refreshPIDName), record); err != nil {
		t.Fatal(err)
	}
	launched := false
	refresher := CLIRefresher{
		Binary: "sh",
		Store:  CredentialStore{Path: staleCredentialFile(t)},
		Cache:  cache.Store{Dir: directory},
		Now:    func() time.Time { return now },
		Command: func(string, ...string) *exec.Cmd {
			launched = true
			return exec.Command("sh", "-c", "exit 0")
		},
	}

	err := refresher.Refresh(context.Background())
	assertSourceError(t, err, source.Transport, "Kimi refresh attempted 2m0s ago — waiting")
	if launched {
		t.Fatal("Refresh() launched despite the recent attempt")
	}
}

func shellCommand(script string) func(string, ...string) *exec.Cmd {
	return func(string, ...string) *exec.Cmd {
		return exec.Command("sh", "-c", script)
	}
}

func writesFutureCredential(t *testing.T, path string) func(string, ...string) *exec.Cmd {
	t.Helper()
	t.Setenv("KIMI_TEST_CREDENTIAL", path)
	t.Setenv("KIMI_TEST_CREDENTIAL_JSON", string(credentialJSON(time.Now().Add(time.Hour))))
	return shellCommand(`printf '%s' "$KIMI_TEST_CREDENTIAL_JSON" > "$KIMI_TEST_CREDENTIAL"`)
}

func assertSourceError(t *testing.T, err error, kind source.ErrorKind, message string) {
	t.Helper()
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != kind || sourceError.Error() != message {
		t.Fatalf("Refresh() error = %v, want %v %q", err, kind, message)
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
