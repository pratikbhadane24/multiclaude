package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pratikbhadane24/multiclaude/internal/fsutil"
	"github.com/pratikbhadane24/multiclaude/internal/paths"
)

const (
	registryFile = "workers.json"
	fileMode     = 0o600
)

// Worker is one machine set up by `ccm worker add`.
type Worker struct {
	Name       string    `json:"name"`
	SSHHost    string    `json:"sshHost"`
	Accounts   []string  `json:"accounts"`
	AddedAt    time.Time `json:"addedAt"`
	LastSyncAt time.Time `json:"lastSyncAt,omitempty"`
}

func registryPath(p paths.Paths) string { return filepath.Join(p.AccountsRoot, registryFile) }

// Load reads the worker list; a missing file is an empty list.
func Load(p paths.Paths) ([]Worker, error) {
	data, err := os.ReadFile(registryPath(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", registryPath(p), err)
	}
	var ws []Worker
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("parse %s: %w", registryPath(p), err)
	}
	return ws, nil
}

// Save writes the worker list atomically with mode 0600.
func Save(p paths.Paths, ws []Worker) error {
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workers: %w", err)
	}
	return fsutil.WriteAtomic(registryPath(p), append(data, '\n'), fileMode)
}

// With returns a new list with w added, replacing any worker of the same name.
func With(ws []Worker, w Worker) []Worker {
	out := make([]Worker, 0, len(ws)+1)
	replaced := false
	for _, x := range ws {
		if x.Name == w.Name {
			out = append(out, w)
			replaced = true
			continue
		}
		out = append(out, x)
	}
	if !replaced {
		out = append(out, w)
	}
	return out
}

// Find returns the worker named name.
func Find(ws []Worker, name string) (Worker, bool) {
	for _, w := range ws {
		if w.Name == name {
			return w, true
		}
	}
	return Worker{}, false
}
