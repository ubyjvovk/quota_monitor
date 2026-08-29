package format_test

import (
	"testing"
	"time"

	"quotamon/internal/format"
)

func TestAgeMatchesQuotaKitBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "future timestamps are just now", duration: -time.Second, want: "just now"},
		{name: "four seconds are just now", duration: 4 * time.Second, want: "just now"},
		{name: "seconds start at five", duration: 5 * time.Second, want: "5s ago"},
		{name: "minutes discard remaining seconds", duration: 3*time.Minute + 59*time.Second, want: "3m ago"},
		{name: "hours discard remaining minutes", duration: 7*time.Hour + 45*time.Minute, want: "7h ago"},
		{name: "days discard remaining hours", duration: 2*24*time.Hour + 23*time.Hour, want: "2d ago"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := format.Age(test.duration); got != test.want {
				t.Fatalf("Age(%s) = %q, want %q", test.duration, got, test.want)
			}
		})
	}
}
