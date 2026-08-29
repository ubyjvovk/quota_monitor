// Package fixtures loads the JSON fixtures shared with QuotaKit tests.
package fixtures

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Load reads a named fixture relative to this source file rather than the cwd.
func Load(name string) ([]byte, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("locate shared fixture %q: runtime caller unavailable", name)
	}
	path := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "QuotaKit", "Tests", "QuotaKitTests", "Fixtures", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load shared fixture %q: %w", name, err)
	}
	return data, nil
}
