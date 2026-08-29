package kimi

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"quotamon/internal/source"
)

const exitDelay = 2 * time.Second

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
	// Run executes the prepared command and defaults to (*exec.Cmd).Run. The
	// command arrives with its directory, environment, and stdin already set.
	Run func(cmd *exec.Cmd) error
}

// Refresh briefly launches the Kimi TUI and succeeds only when the CLI-owned
// credential file contains a future expiry afterward. The process exit status
// is deliberately ignored because /exit does not reliably terminate every CLI
// release.
//
// The CLI runs from the user's home directory: launched from an untrusted
// working directory the TUI stops at its "Trust this folder?" prompt and
// never reaches the token refresh it performs on startup. The launch also
// runs on its own deadline detached from the caller's context, so a caller's
// cancel cannot kill the CLI in the middle of rewriting its credential file.
func (r CLIRefresher) Refresh(ctx context.Context) error {
	binary := r.Binary
	if binary == "" {
		resolved, err := exec.LookPath("kimi")
		if err != nil {
			return source.Errorf(source.NotConfigured, "Kimi CLI not found — install and open `kimi` to refresh sign-in")
		}
		binary = resolved
	}

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
	run := r.Run
	if run == nil {
		run = runCommand
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return source.Errorf(source.NotConfigured, "Kimi CLI auto-refresh needs the home directory — open `kimi` manually (%v)", err)
	}

	runContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), deadline)
	defer cancel()
	command := exec.CommandContext(runContext, argv[0], argv[1:]...)
	command.Dir = home
	command.Env = refreshEnvironment()
	command.Stdin = &refreshInput{startup: startup, data: []byte("/exit\r")}
	runErr := run(command)

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

func runCommand(cmd *exec.Cmd) error {
	return cmd.Run()
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
