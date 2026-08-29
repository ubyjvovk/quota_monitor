package grok

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

const defaultBaseURL = "https://cli-chat-proxy.grok.com"

// LiveSource reads Grok usage from the cli-chat-proxy billing endpoint.
type LiveSource struct {
	// BaseURL is the Grok billing API origin and defaults to https://cli-chat-proxy.grok.com.
	BaseURL string
	// Client performs the HTTP request and defaults to a client with a 15-second timeout.
	Client *http.Client
	// Credentials returns raw Grok credential JSON and defaults to reading DefaultAuthPath.
	Credentials func() ([]byte, error)
}

// ProviderID returns Grok's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns Grok's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries and normalises Grok's shared-pool billing endpoint.
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	credentialsProvider := s.Credentials
	if credentialsProvider == nil {
		credentialsProvider = readCredentials
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/billing?format=credits", nil)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not create Grok billing request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+credentials.Token)
	request.Header.Set("x-grok-client-mode", "grok-build")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "quotamon/0.1")

	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not reach Grok billing endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return snapshot.Provider{}, source.Errorf(source.Unauthorized, "Grok sign-in expired — run `grok login`")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return snapshot.Provider{}, source.ForHTTP(response.StatusCode, DisplayName)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not read Grok billing response: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from Grok billing endpoint")
	}
	provider, ok := Snapshot(root, time.Now())
	if !ok {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from Grok billing endpoint")
	}
	return provider, nil
}

func readCredentials() ([]byte, error) {
	path := DefaultAuthPath()
	blob, err := os.ReadFile(path)
	if err == nil {
		return blob, nil
	}
	if os.IsNotExist(err) {
		return nil, noTokenError()
	}
	return nil, source.Errorf(source.Transport, "Could not read Grok credentials at %s: %v", path, err)
}
