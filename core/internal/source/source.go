// Package source defines provider sources and actionable error priorities.
package source

import (
	"context"
	"errors"
	"fmt"

	"quotamon/internal/snapshot"
)

// Source obtains one provider's normalised quota snapshot.
type Source interface {
	// ProviderID returns the stable provider identifier.
	ProviderID() string
	// DisplayName returns the provider name shown to users.
	DisplayName() string
	// Origin identifies whether the source is live or local.
	Origin() snapshot.Origin
	// Fetch obtains the provider snapshot or an actionable error.
	Fetch(ctx context.Context) (snapshot.Provider, error)
}

// ErrorKind ranks source failures by their reporting priority.
type ErrorKind int

const (
	// NotConfigured means an optional fallback was not set up.
	NotConfigured ErrorKind = iota
	// NoDataFound means a configured source contained no usable reading.
	NoDataFound
	// Transport means an immediate retry could plausibly succeed.
	Transport
	// Malformed means a source returned data QuotaMon could not understand.
	Malformed
	// Unauthorized means the user must authenticate again.
	Unauthorized
)

// Error is an actionable provider failure with a reporting priority.
type Error struct {
	// Kind determines which failure should be surfaced when several occur.
	Kind ErrorKind
	// Message tells the user how the source failed or what to do next.
	Message string
}

// Error returns the actionable failure message.
func (e *Error) Error() string {
	return e.Message
}

// Errorf formats a source Error of the supplied kind.
func Errorf(kind ErrorKind, format string, arguments ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, arguments...)}
}

// ForHTTP maps an HTTP response status to QuotaKit's actionable failure semantics.
func ForHTTP(status int, provider string) *Error {
	switch {
	case status == 401 || status == 403:
		return Errorf(Unauthorized, "%s rejected the token — sign in again", provider)
	case status == 429:
		return Errorf(Transport, "%s is rate limiting usage checks — will retry", provider)
	case status >= 500 && status <= 599:
		return Errorf(Transport, "%s usage endpoint is unavailable (HTTP %d)", provider, status)
	default:
		return Errorf(Transport, "%s usage endpoint returned HTTP %d", provider, status)
	}
}

// Priority returns a source Error's kind or transport priority for other errors.
func Priority(err error) int {
	var sourceError *Error
	if errors.As(err, &sourceError) {
		return int(sourceError.Kind)
	}
	return int(Transport)
}
