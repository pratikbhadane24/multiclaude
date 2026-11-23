package t3

import (
	"slices"

	"github.com/pratikbhadane24/multiclaude/internal/paths"
	"github.com/pratikbhadane24/multiclaude/internal/registry"
)

// HasInstance reports whether T3's settings already hold an instance for a
// (matched by homePath, as Sync does). A missing settings file means false.
// It is read-only.
func HasInstance(p paths.Paths, a registry.Account, shimPath string) (bool, error) {
	data, _, existed, err := readSettings(p.T3Settings)
	if err != nil || !existed {
		return false, err
	}
	_, added, _, err := Merge(data, []registry.Account{a}, shimPath)
	if err != nil {
		return false, err
	}
	return !slices.Contains(added, instanceID(a)), nil
}
