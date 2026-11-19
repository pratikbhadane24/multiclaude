package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"time"
)

// fileStamp identifies a file version cheaply (exists, size, mtime).
type fileStamp struct {
	exists bool
	size   int64
	mod    time.Time
}

func (s fileStamp) equal(o fileStamp) bool {
	return s.exists == o.exists && s.size == o.size && s.mod.Equal(o.mod)
}

// stampOf returns the stamp of path; a missing file has the zero stamp.
func stampOf(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fileStamp{}, nil
	}
	if err != nil {
		return fileStamp{}, fmt.Errorf("stat %s: %w", path, err)
	}
	return fileStamp{exists: true, size: fi.Size(), mod: fi.ModTime()}, nil
}

// readState returns state.json's members and its mcpSynced map; missing = empty.
func readState(path string) ([]member, map[string][]string, error) {
	synced := map[string][]string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, synced, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	members, err := parseObject(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if raw, ok := lookup(members, syncedKey); ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &synced); err != nil {
			return nil, nil, fmt.Errorf("parse %s in %s: %w", syncedKey, path, err)
		}
	}
	return members, synced, nil
}

// updateState records names for dir, writing only when they differ from cur.
func updateState(path string, members []member, synced map[string][]string, dir string, cur, names []string) error {
	if slices.Equal(cur, names) {
		return nil
	}
	next := make(map[string][]string, len(synced)+1)
	for k, v := range synced {
		next[k] = v
	}
	next[dir] = names
	val, err := encodeIndented(next)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data, err := renderObject(withMember(members, syncedKey, val))
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeAtomic(path, data, nil)
}
