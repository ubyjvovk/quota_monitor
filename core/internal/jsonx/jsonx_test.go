package jsonx_test

import (
	"testing"
	"time"

	"quotamon/internal/jsonx"
)

func TestTimeCoercesProviderTimestampShapes(t *testing.T) {
	parsedRFC3339, err := time.Parse(time.RFC3339Nano, "2026-08-29T18:59:59.741925+00:00")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		input any
		want  time.Time
	}{
		{name: "epoch seconds", input: float64(1_788_038_896), want: time.Unix(1_788_038_896, 0).UTC()},
		{name: "epoch milliseconds", input: float64(1_785_567_600_000), want: time.UnixMilli(1_785_567_600_000).UTC()},
		{name: "RFC 3339 with fractions", input: "2026-08-29T18:59:59.741925+00:00", want: parsedRFC3339},
		{name: "integer epoch seconds", input: 1_788_038_896, want: time.Unix(1_788_038_896, 0).UTC()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := jsonx.Time(test.input)
			if !ok || !got.Equal(test.want) {
				t.Fatalf("Time(%v) = (%s, %v), want (%s, true)", test.input, got, ok, test.want)
			}
		})
	}
}

func TestGetWalksOnlyTheRequestedMapPath(t *testing.T) {
	root, err := jsonx.Parse([]byte(`{"outer":{"nested":{"answer":42},"nil":null,"scalar":"stop"},"answer":99}`))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path []string
		ok   bool
	}{
		{name: "explicit nested path is found", path: []string{"outer", "nested", "answer"}, ok: true},
		{name: "missing path is absent", path: []string{"outer", "missing"}},
		{name: "nested nil is absent", path: []string{"outer", "nil"}},
		{name: "key below a scalar is not searched elsewhere", path: []string{"outer", "scalar", "answer"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, ok := jsonx.Get(root, test.path...)
			if ok != test.ok {
				t.Fatalf("Get(%v) ok = %v, want %v", test.path, ok, test.ok)
			}
		})
	}
}

func TestScalarHelpersDoNotCoerceTypes(t *testing.T) {
	t.Run("string accepts only a string", func(t *testing.T) {
		if got, ok := jsonx.String("value"); !ok || got != "value" {
			t.Fatalf("String() = (%q, %v)", got, ok)
		}
		if _, ok := jsonx.String(true); ok {
			t.Fatal("String() accepted a boolean")
		}
	})
	t.Run("float accepts only a float64", func(t *testing.T) {
		if got, ok := jsonx.Float(float64(1.5)); !ok || got != 1.5 {
			t.Fatalf("Float() = (%v, %v)", got, ok)
		}
		if _, ok := jsonx.Float(1); ok {
			t.Fatal("Float() accepted an int")
		}
	})
	t.Run("int accepts only an integral float64", func(t *testing.T) {
		if got, ok := jsonx.Int(float64(42)); !ok || got != 42 {
			t.Fatalf("Int() = (%v, %v)", got, ok)
		}
		if _, ok := jsonx.Int(42.5); ok {
			t.Fatal("Int() accepted a fractional value")
		}
	})
	t.Run("bool accepts only a boolean", func(t *testing.T) {
		if got, ok := jsonx.Bool(true); !ok || !got {
			t.Fatalf("Bool() = (%v, %v)", got, ok)
		}
		if _, ok := jsonx.Bool("true"); ok {
			t.Fatal("Bool() accepted a string")
		}
	})
}
