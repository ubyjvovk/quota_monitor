package kimi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"quotamon/internal/cache"
	"quotamon/internal/source"
)

const (
	exitDelay       = 2 * time.Second
	refreshRateCap  = 10 * time.Minute
	refreshLockName = "kimi-refresh.lock"
	refreshPIDName  = "kimi-refresh.json"
	refreshLastName = "kimi-refresh.last"
)

// CLIRefresher launches the Kimi CLI in a pseudo-terminal so the CLI can
// rotate and persist its own credentials. QuotaMon never handles the refresh
// token or calls Kimi's token endpoint directly.
type CLIRefresher struct {
	// Binary is the Kimi executable and defaults to LookPath("kimi").
	Binary string
	// Startup is the delay before sending /exit and defaults to eight seconds.
	Startup time.Duration
	// Deadline bounds the CLI process and defaults to twenty seconds.
	Deadline time.Duration
	// Store is re-read after the process to verify that the CLI renewed its token.
	Store CredentialStore
	// Cache locates the coordination files beside QuotaMon's cached readings.
	Cache cache.Store
	// Command constructs the process and defaults to exec.Command. Tests may
	// replace it while retaining the production start, wait, and kill lifecycle.
	Command func(name string, arg ...string) *exec.Cmd
	// GroupCommand returns the command line for a process group and defaults to
	// querying ps. Tests replace it because the worker sandbox denies ps access.
	GroupCommand func(pgid int) (string, error)
	// Now returns the current time and defaults to time.Now.
	Now func() time.Time
}

// Refresh briefly launches the Kimi TUI and succeeds only when the CLI-owned
// credential file contains a future expiry afterward. Four guards keep a
// failed TUI launch contained: a non-blocking lock excludes concurrent runs, a
// recorded and command-verified orphan group is swept before launch, a
// ten-minute rate cap prevents launch loops, and a private process group is
// terminated on cancellation or deadline. The process exit status is
// deliberately ignored because /exit does not reliably terminate every CLI
// release.
//
// The CLI runs from the user's home directory: launched from an untrusted
// working directory the TUI stops at its "Trust this folder?" prompt and
// never reaches the token refresh it performs on startup.
func (r CLIRefresher) Refresh(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		return source.Errorf(source.NotConfigured, "Kimi auto-refresh is not supported on Windows — open `kimi` to refresh")
	}

	directory := r.Cache.Directory()
	lock, err := acquireRefreshLock(directory)
	if err != nil {
		return err
	}
	defer releaseRefreshLock(lock)

	binary := r.Binary
	if binary == "" {
		binary = "kimi"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return source.Errorf(source.NotConfigured, "Kimi CLI not found — install and open `kimi` to refresh sign-in")
	}
	binary = resolved

	argv, err := refreshArgv(binary)
	if err != nil {
		return err
	}
	startup := r.Startup
	if startup == 0 {
		startup = 8 * time.Second
	}
	deadline := r.Deadline
	if deadline == 0 {
		deadline = 20 * time.Second
	}
	commandFactory := r.Command
	if commandFactory == nil {
		commandFactory = exec.Command
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}

	pidPath := filepath.Join(directory, refreshPIDName)
	lastPath := filepath.Join(directory, refreshLastName)
	record, hasRecord := readRefreshRecord(pidPath)
	if hasRecord && refreshGroupIsOurs(record.PGID, binary, r.GroupCommand) {
		_ = signalRefreshGroup(record.PGID, syscall.SIGKILL)
		_ = os.Remove(pidPath)
	}
	started, attempted := mostRecentAttempt(record, hasRecord, lastPath)
	if attempted {
		age := now().Sub(started)
		if age < refreshRateCap {
			if age < 0 {
				age = 0
			}
			return source.Errorf(source.Transport, "Kimi refresh attempted %s ago — waiting", formatRefreshAge(age))
		}
	}
	_ = os.Remove(pidPath)

	home, err := os.UserHomeDir()
	if err != nil {
		return source.Errorf(source.NotConfigured, "Kimi CLI auto-refresh needs the home directory — open `kimi` manually (%v)", err)
	}

	command := commandFactory(argv[0], argv[1:]...)
	command.Dir = home
	command.Env = refreshEnvironment()
	command.Stdin = &refreshInput{startup: startup, data: []byte("/exit\r")}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return source.Errorf(source.Transport, "Kimi CLI refresh could not start — open `kimi` (%v)", err)
	}
	pgid, err := syscall.Getpgid(command.Process.Pid)
	if err != nil {
		pgid = command.Process.Pid
	}
	record = refreshRecord{PGID: pgid, Started: now().UTC().Truncate(time.Second).Format(time.RFC3339)}
	if err := writeRefreshRecord(pidPath, record); err != nil {
		terminateRefreshGroup(command, pgid)
		return source.Errorf(source.Transport, "Kimi CLI refresh could not record its process — open `kimi` (%v)", err)
	}
	defer os.Remove(pidPath)
	if err := writeRefreshStamp(lastPath, record.Started); err != nil {
		terminateRefreshGroup(command, pgid)
		return source.Errorf(source.Transport, "Kimi CLI refresh could not record its attempt — open `kimi` (%v)", err)
	}
	runErr := waitForRefresh(command, pgid, ctx, deadline)

	credentials, credentialErr := r.Store.Load()
	if credentialErr == nil && credentials.ExpiresAt != nil && credentials.ExpiresAt.After(time.Now()) {
		return nil
	}
	if credentialErr != nil {
		return source.Errorf(source.NotConfigured, "Kimi CLI did not refresh sign-in — open `kimi` (%v)", credentialErr)
	}
	if runErr != nil {
		return source.Errorf(source.NotConfigured, "Kimi CLI did not refresh sign-in — open `kimi` (%v)", runErr)
	}
	return source.Errorf(source.NotConfigured, "Kimi CLI did not refresh sign-in — open `kimi`")
}

type refreshRecord struct {
	PGID    int    `json:"pgid"`
	Started string `json:"started"`
}

func acquireRefreshLock(directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, source.Errorf(source.Transport, "Kimi refresh could not create its cache directory (%v)", err)
	}
	path := filepath.Join(directory, refreshLockName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Kimi refresh could not open its lock (%v)", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, source.Errorf(source.Transport, "Kimi refresh already in progress")
		}
		return nil, source.Errorf(source.Transport, "Kimi refresh could not acquire its lock (%v)", err)
	}
	return file, nil
}

func releaseRefreshLock(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func readRefreshRecord(path string) (refreshRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return refreshRecord{}, false
	}
	var record refreshRecord
	if json.Unmarshal(data, &record) != nil || record.PGID <= 1 {
		return refreshRecord{}, false
	}
	if _, err := time.Parse(time.RFC3339, record.Started); err != nil {
		return refreshRecord{}, false
	}
	return record, true
}

func writeRefreshRecord(path string, record refreshRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func writeRefreshStamp(path, started string) error {
	return os.WriteFile(path, []byte(started+"\n"), 0o600)
}

func mostRecentAttempt(record refreshRecord, hasRecord bool, lastPath string) (time.Time, bool) {
	var mostRecent time.Time
	if hasRecord {
		mostRecent, _ = time.Parse(time.RFC3339, record.Started)
	}
	data, err := os.ReadFile(lastPath)
	if err == nil {
		stamp, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
		if parseErr == nil && stamp.After(mostRecent) {
			mostRecent = stamp
		}
	}
	return mostRecent, !mostRecent.IsZero()
}

func formatRefreshAge(age time.Duration) string {
	return age.Round(time.Second).String()
}

func refreshGroupIsOurs(pgid int, binary string, inspect func(int) (string, error)) bool {
	if pgid <= 1 || syscall.Kill(-pgid, 0) != nil {
		return false
	}
	if inspect == nil {
		inspect = refreshGroupCommand
	}
	command, err := inspect(pgid)
	if err != nil {
		return false
	}
	return strings.Contains(command, "script") && strings.Contains(command, binary)
}

func refreshGroupCommand(pgid int) (string, error) {
	output, err := exec.Command("ps", "-o", "command=", "-g", strconv.Itoa(pgid)).Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func signalRefreshGroup(pgid int, signal syscall.Signal) error {
	if pgid <= 1 {
		return fmt.Errorf("refuse to signal process group %d", pgid)
	}
	return syscall.Kill(-pgid, signal)
}

func waitForRefresh(command *exec.Cmd, pgid int, ctx context.Context, deadline time.Duration) error {
	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
	}()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case err := <-waited:
		return err
	case <-ctx.Done():
	case <-timer.C:
	}
	_ = signalRefreshGroup(pgid, syscall.SIGTERM)
	time.Sleep(exitDelay)
	_ = signalRefreshGroup(pgid, syscall.SIGKILL)
	return <-waited
}

func terminateRefreshGroup(command *exec.Cmd, pgid int) {
	_ = signalRefreshGroup(pgid, syscall.SIGTERM)
	time.Sleep(exitDelay)
	_ = signalRefreshGroup(pgid, syscall.SIGKILL)
	_ = command.Wait()
}

func refreshArgv(binary string) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return []string{"script", "-q", "/dev/null", binary}, nil
	case "linux":
		return []string{"script", "-q", "-c", binary, "/dev/null"}, nil
	default:
		return nil, source.Errorf(source.NotConfigured, "Kimi CLI auto-refresh is unavailable on %s — open `kimi` manually", runtime.GOOS)
	}
}

// refreshEnvironment hands the TUI a terminal type when the caller (a Waybar
// module or a launchd job) has none; without TERM the CLI cannot draw.
func refreshEnvironment() []string {
	environment := os.Environ()
	if _, ok := os.LookupEnv("TERM"); !ok {
		environment = append(environment, "TERM=xterm-256color")
	}
	return environment
}

type refreshInput struct {
	startup time.Duration
	data    []byte
	started bool
	closed  bool
}

func (r *refreshInput) Read(destination []byte) (int, error) {
	if !r.started {
		time.Sleep(r.startup)
		r.started = true
	}
	if len(r.data) > 0 {
		count := copy(destination, r.data)
		r.data = r.data[count:]
		return count, nil
	}
	if !r.closed {
		time.Sleep(exitDelay)
		r.closed = true
	}
	return 0, io.EOF
}
