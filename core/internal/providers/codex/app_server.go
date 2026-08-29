package codex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const appServerRequests = "{\"method\":\"initialize\",\"id\":1,\"params\":{\"clientInfo\":{\"name\":\"quotamon\",\"title\":\"Quota Monitor\",\"version\":\"0.1.0\"}}}\n" +
	"{\"method\":\"initialized\"}\n" +
	"{\"method\":\"account/rateLimits/read\",\"id\":2}\n"

// AppServerSource reads live quota from the Codex CLI's local JSON-RPC server.
type AppServerSource struct {
	// Run executes the three-line exchange and returns raw stdout.
	Run func(ctx context.Context, requests []byte) ([]byte, error)
}

// ProviderID returns the stable ChatGPT provider identifier.
func (AppServerSource) ProviderID() string { return ProviderID }

// DisplayName returns the provider name shown to users.
func (AppServerSource) DisplayName() string { return DisplayName }

// Origin identifies app-server readings as live.
func (AppServerSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch performs the app-server exchange and normalises its rate limits.
func (s AppServerSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	run := s.Run
	if run == nil {
		run = runAppServer
	}
	runContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stdout, err := run(runContext, []byte(appServerRequests))
	if err != nil {
		if errors.Is(runContext.Err(), context.DeadlineExceeded) {
			return snapshot.Provider{}, source.Errorf(source.Transport, "codex app-server timed out")
		}
		var sourceError *source.Error
		if errors.As(err, &sourceError) {
			return snapshot.Provider{}, sourceError
		}
		return snapshot.Provider{}, source.Errorf(source.Transport, "codex app-server failed: %v", err)
	}

	limits, err := ParseAppServerOutput(stdout)
	if err != nil {
		return snapshot.Provider{}, err
	}
	provider, ok := Snapshot(limits, time.Now(), snapshot.OriginLive)
	if !ok {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised app-server rate limits")
	}
	return provider, nil
}

// ParseAppServerOutput selects the id:2 reply and returns result.rateLimits.
func ParseAppServerOutput(stdout []byte) (any, error) {
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		root, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			continue
		}
		idValue, ok := jsonx.Get(root, "id")
		if !ok {
			continue
		}
		id, ok := jsonx.Float(idValue)
		if !ok || id != 2 {
			continue
		}

		if errorValue, hasError := jsonx.Get(root, "error"); hasError {
			message := "codex app-server returned an error"
			if value, found := jsonx.Get(errorValue, "message"); found {
				if parsed, valid := jsonx.String(value); valid && parsed != "" {
					message = parsed
				}
			}
			return nil, source.Errorf(source.Transport, "codex app-server error: %s", message)
		}
		limits, ok := jsonx.Get(root, "result", "rateLimits")
		if !ok {
			return nil, source.Errorf(source.Malformed, "codex app-server id:2 reply has no result.rateLimits")
		}
		return limits, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, source.Errorf(source.Malformed, "read codex app-server output: %v", err)
	}
	return nil, source.Errorf(source.Malformed, "codex app-server returned no id:2 rate-limits reply")
}

func runAppServer(ctx context.Context, requests []byte) ([]byte, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return nil, source.Errorf(source.NotConfigured, "Codex CLI not installed — install it or use `codex login`")
	}

	command := exec.CommandContext(ctx, path, "app-server", "--stdio")
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return nil, err
	}
	if _, err := stdin.Write(requests); err != nil {
		_ = stdin.Close()
		_ = command.Wait()
		return nil, err
	}

	var output bytes.Buffer
	foundReply := false
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = output.Write(line)
		_ = output.WriteByte('\n')

		root, err := jsonx.Parse(line)
		if err != nil {
			continue
		}
		idValue, ok := jsonx.Get(root, "id")
		if !ok {
			continue
		}
		id, ok := jsonx.Float(idValue)
		if ok && id == 2 {
			foundReply = true
			break
		}
	}
	scanErr := scanner.Err()
	closeErr := stdin.Close()

	if !foundReply {
		_ = command.Wait()
		if scanErr != nil {
			return nil, scanErr
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return output.Bytes(), nil
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- command.Wait()
	}()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case waitErr := <-waitDone:
		if closeErr != nil {
			return nil, closeErr
		}
		if waitErr != nil {
			return nil, waitErr
		}
	case <-timer.C:
		_ = command.Process.Kill()
		<-waitDone
	}
	return output.Bytes(), nil
}
