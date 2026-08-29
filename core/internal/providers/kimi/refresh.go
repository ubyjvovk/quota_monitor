package kimi

import (
	"context"
	"fmt"
	"io"
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
	// Run launches argv with stdin and defaults to an os/exec command.
	Run func(ctx context.Context, argv []string, stdin io.Reader) error
}

// Refresh briefly launches the Kimi TUI and succeeds only when the CLI-owned
// credential file contains a future expiry afterward. The process exit status
// is deliberately ignored because /exit does not reliably terminate every CLI release.
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

	runContext, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	runErr := run(runContext, argv, &refreshInput{startup: startup, data: []byte("/exit\r")})

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

func runCommand(ctx context.Context, argv []string, stdin io.Reader) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty Kimi CLI command")
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Stdin = stdin
	return command.Run()
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
