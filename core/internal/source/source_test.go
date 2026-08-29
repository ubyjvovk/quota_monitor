package source_test

import (
	"errors"
	"fmt"
	"testing"

	"quotamon/internal/source"
)

func TestForHTTPMatchesQuotaKitErrorSemantics(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		wantKind source.ErrorKind
		wantText string
	}{
		{name: "401 is unauthorized", status: 401, wantKind: source.Unauthorized, wantText: "Claude rejected the token — sign in again"},
		{name: "403 is unauthorized", status: 403, wantKind: source.Unauthorized, wantText: "Claude rejected the token — sign in again"},
		{name: "429 is transport", status: 429, wantKind: source.Transport, wantText: "Claude is rate limiting usage checks — will retry"},
		{name: "503 is transport", status: 503, wantKind: source.Transport, wantText: "Claude usage endpoint is unavailable (HTTP 503)"},
		{name: "418 is transport", status: 418, wantKind: source.Transport, wantText: "Claude usage endpoint returned HTTP 418"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := source.ForHTTP(test.status, "Claude")
			if got.Kind != test.wantKind || got.Error() != test.wantText {
				t.Fatalf("ForHTTP() = (%v, %q), want (%v, %q)", got.Kind, got.Error(), test.wantKind, test.wantText)
			}
		})
	}
}

func TestPriorityUsesSourceKindsAndTreatsOtherErrorsAsTransport(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "source kind supplies its numeric priority", err: source.Errorf(source.Unauthorized, "expired"), want: int(source.Unauthorized)},
		{name: "wrapped source error keeps its priority", err: fmt.Errorf("fetch: %w", source.Errorf(source.Malformed, "bad JSON")), want: int(source.Malformed)},
		{name: "ordinary error has transport priority", err: errors.New("socket closed"), want: int(source.Transport)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := source.Priority(test.err); got != test.want {
				t.Fatalf("Priority() = %d, want %d", got, test.want)
			}
		})
	}
}
