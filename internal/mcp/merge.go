// Package mcp copies user-scope MCP servers from the main ~/.claude.json into
// an account's own .claude.json (one-way; main wins on a name clash).
package mcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
)

// Merge computes an account's new mcpServers: every server from main, plus the
// account's own servers, minus servers that came from main earlier (prevSynced)
// but are gone from main now. synced is the sorted list of names taken from
// main. changed reports whether servers differs from account (ignoring JSON
// whitespace). Inputs are never mutated.
func Merge(main, account map[string]json.RawMessage, prevSynced []string) (servers map[string]json.RawMessage, synced []string, changed bool) {
	stale := make(map[string]bool, len(prevSynced))
	for _, name := range prevSynced {
		if _, ok := main[name]; !ok {
			stale[name] = true
		}
	}
	servers = make(map[string]json.RawMessage, len(main)+len(account))
	for name, v := range account {
		if !stale[name] {
			servers[name] = v
		}
	}
	synced = make([]string, 0, len(main))
	for name, v := range main {
		servers[name] = v
		synced = append(synced, name)
	}
	sort.Strings(synced)
	return servers, synced, !sameServers(servers, account)
}

func sameServers(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for name, av := range a {
		bv, ok := b[name]
		if !ok || !sameJSON(av, bv) {
			return false
		}
	}
	return true
}

// sameJSON compares two JSON values semantically: whitespace and string
// escaping (e.g. "&" vs "&") are ignored; numbers compare by their text.
func sameJSON(a, b json.RawMessage) bool {
	va, errA := decodeValue(a)
	vb, errB := decodeValue(b)
	if errA != nil || errB != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(va, vb)
}

func decodeValue(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}
