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

func TestCountdownMatchesQuotaKitStrings(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "negative durations are under one minute", duration: -time.Second, want: "<1m"},
		{name: "seconds are under one minute", duration: 59 * time.Second, want: "<1m"},
		{name: "minutes discard remaining seconds", duration: 42*time.Minute + 59*time.Second, want: "42m"},
		{name: "hours include remaining minutes", duration: 10*time.Hour + 11*time.Minute + 59*time.Second, want: "10h 11m"},
		{name: "days include remaining hours", duration: 6*24*time.Hour + 7*time.Hour + 59*time.Minute, want: "6d 7h"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := format.Countdown(test.duration); got != test.want {
				t.Fatalf("Countdown(%s) = %q, want %q", test.duration, got, test.want)
			}
		})
	}
}

func TestPercentRoundsLikeQuotaKit(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  string
	}{
		{name: "whole percentages stay whole", value: 43, want: "43%"},
		{name: "fractions below half round down", value: 18.4, want: "18%"},
		{name: "half percentages round up", value: 92.5, want: "93%"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := format.Percent(test.value); got != test.want {
				t.Fatalf("Percent(%v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
