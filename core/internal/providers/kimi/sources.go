package kimi

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

// defaultBaseURL is Kimi's coding quota API origin, from ~/.kimi-code/config.toml.
// The endpoint path is /usages (plural) — the singular route is 404.
const defaultBaseURL = "https://api.kimi.com/coding/v1"

// LiveSource reads Kimi coding usage from its authenticated /usages endpoint.
type LiveSource struct {
	// BaseURL is the Kimi coding API origin and defaults to https://api.kimi.com/coding/v1.
	BaseURL string
	// Client performs the HTTP request and defaults to a client with a 15-second timeout.
	Client *http.Client
	// Credentials returns raw Kimi credential JSON and defaults to reading DefaultCredentialPath.
	Credentials func() ([]byte, error)
}

// ProviderID returns Kimi's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns Kimi's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries and normalises Kimi's coding usage endpoint. The Kimi bearer
// token lives only 15 minutes and the TUI refreshes it on launch, so an expired
// token is an authentication failure whose fix is to run `kimi` again, never a
// raw status code.
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	credentialsProvider := s.Credentials
	if credentialsProvider == nil {
		credentialsProvider = (CredentialStore{}).Read
	}
	blob, err := credentialsProvider()
	if err != nil {
		return snapshot.Provider{}, err
	}
	credentials, err := ParseCredentials(blob)
	if err != nil {
		return snapshot.Provider{}, err
	}

	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/usages", nil)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not create Kimi usage request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+credentials.Token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "quotamon/0.1")

	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not reach Kimi usage endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return snapshot.Provider{}, source.Errorf(source.Unauthorized, "Kimi sign-in expired — run `kimi` once to refresh it")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return snapshot.Provider{}, source.ForHTTP(response.StatusCode, DisplayName)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not read Kimi usage response: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from Kimi usage endpoint")
	}
	provider, ok := Snapshot(root, time.Now())
	if !ok {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from Kimi usage endpoint")
	}
	return provider, nil
}

func readCredentials() ([]byte, error) {
	return (CredentialStore{}).Read()
}

// CredentialStore reads and parses the credential file owned by the Kimi CLI.
// An empty Path uses DefaultCredentialPath.
type CredentialStore struct {
	Path string
}

// Read returns the raw Kimi credential JSON.
func (s CredentialStore) Read() ([]byte, error) {
	path := s.Path
	if path == "" {
		path = DefaultCredentialPath()
	}
	blob, err := os.ReadFile(path)
	if err == nil {
		return blob, nil
	}
	if os.IsNotExist(err) {
		return nil, noTokenError()
	}
	return nil, source.Errorf(source.Transport, "Could not read Kimi credentials at %s: %v", path, err)
}

// Load returns the parsed Kimi credentials from the CLI-owned file.
func (s CredentialStore) Load() (Credentials, error) {
	blob, err := s.Read()
	if err != nil {
		return Credentials{}, err
	}
	return ParseCredentials(blob)
}
