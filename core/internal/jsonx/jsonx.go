// Package jsonx provides explicit-path access to loosely typed provider JSON.
package jsonx

import (
	"encoding/json"
	"math"
	"time"
)

// Parse decodes JSON into maps, slices, float64 values, strings, booleans, and nil.
func Parse(data []byte) (any, error) {
	var value any
	err := json.Unmarshal(data, &value)
	return value, err
}

// Get walks an explicit map path and reports false for missing or nil values.
func Get(value any, path ...string) (any, bool) {
	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[key]
		if !ok || current == nil {
			return nil, false
		}
	}
	if current == nil {
		return nil, false
	}
	return current, true
}

// String returns a string value without coercion.
func String(value any) (string, bool) {
	result, ok := value.(string)
	return result, ok
}

// Float returns a float64 value without coercion.
func Float(value any) (float64, bool) {
	result, ok := value.(float64)
	return result, ok
}

// Int returns an integral float64 as an int.
func Int(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
		return 0, false
	}
	result := int(number)
	if float64(result) != number {
		return 0, false
	}
	return result, true
}

// Bool returns a boolean value without coercion.
func Bool(value any) (bool, bool) {
	result, ok := value.(bool)
	return result, ok
}

// Time coerces epoch seconds, epoch milliseconds, or an RFC 3339 string.
func Time(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case float64:
		return numericTime(typed)
	case int:
		return numericTime(float64(typed))
	case int64:
		return numericTime(float64(typed))
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			return time.Time{}, false
		}
		return parsed, true
	default:
		return time.Time{}, false
	}
}

func numericTime(value float64) (time.Time, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return time.Time{}, false
	}
	seconds := value
	if value > 1e11 {
		seconds = value / 1000
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fraction*float64(time.Second))).UTC(), true
}
