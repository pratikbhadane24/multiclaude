// Package worker sets up and checks other machines that run Claude for
// the hub ("workers") over SSH.
package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const ccmBinName = "ccm"

// MergeSettings returns the hub's settings.json adapted for a worker:
// a statusLine whose command starts with a ccm binary is pointed at
// workerCCM, hooks are the union of hub and worker hooks (grouped by event
// and matcher, deduplicated by command, hub order first), and keys that
// exist only in the worker file are kept. worker may be empty.
func MergeSettings(hub, worker []byte, workerCCM string) ([]byte, error) {
	h, err := decodeObject(hub)
	if err != nil {
		return nil, fmt.Errorf("hub settings.json: %w", err)
	}
	w := map[string]any{}
	if len(bytes.TrimSpace(worker)) > 0 {
		if w, err = decodeObject(worker); err != nil {
			return nil, fmt.Errorf("worker settings.json: %w", err)
		}
	}
	out := make(map[string]any, len(h)+len(w))
	for k, v := range w {
		out[k] = v
	}
	for k, v := range h {
		out[k] = v
	}
	if sl, ok := h["statusLine"].(map[string]any); ok {
		out["statusLine"] = rewriteStatusLine(sl, workerCCM)
	}
	out["hooks"] = mergeHooks(asObject(h["hooks"]), asObject(w["hooks"]))
	if len(asObject(out["hooks"])) == 0 {
		delete(out, "hooks")
	}
	return encodeObject(out)
}

func decodeObject(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func encodeObject(m map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// rewriteStatusLine returns a copy of sl whose leading ccm binary path is
// replaced by workerCCM; other commands are returned unchanged.
func rewriteStatusLine(sl map[string]any, workerCCM string) map[string]any {
	out := make(map[string]any, len(sl))
	for k, v := range sl {
		out[k] = v
	}
	cmd, ok := sl["command"].(string)
	if !ok {
		return out
	}
	first, rest, _ := strings.Cut(cmd, " ")
	if filepath.Base(first) != ccmBinName {
		return out
	}
	out["command"] = strings.TrimSpace(workerCCM + " " + rest)
	return out
}

// mergeHooks unions two hooks objects (event -> [{matcher, hooks: [...]}]).
func mergeHooks(hub, worker map[string]any) map[string]any {
	out := map[string]any{}
	for _, src := range []map[string]any{hub, worker} {
		for event, groups := range src {
			out[event] = mergeGroups(asList(out[event]), asList(groups))
		}
	}
	return out
}

// mergeGroups adds groups into base, joining groups with the same matcher
// and skipping hooks whose command is already present in that group.
func mergeGroups(base, add []any) []any {
	out := make([]any, 0, len(base)+len(add))
	for _, g := range base {
		out = append(out, copyGroup(asObject(g)))
	}
	for _, g := range add {
		grp := asObject(g)
		i := findGroup(out, grp["matcher"])
		if i < 0 {
			out = append(out, copyGroup(grp))
			continue
		}
		target := asObject(out[i])
		target["hooks"] = appendNewHooks(asList(target["hooks"]), asList(grp["hooks"]))
	}
	return out
}

func copyGroup(g map[string]any) map[string]any {
	out := make(map[string]any, len(g))
	for k, v := range g {
		out[k] = v
	}
	out["hooks"] = append([]any(nil), asList(g["hooks"])...)
	return out
}

func findGroup(groups []any, matcher any) int {
	for i, g := range groups {
		if fmt.Sprint(asObject(g)["matcher"]) == fmt.Sprint(matcher) {
			return i
		}
	}
	return -1
}

func appendNewHooks(have, add []any) []any {
	seen := map[string]bool{}
	for _, h := range have {
		seen[fmt.Sprint(asObject(h)["command"])] = true
	}
	out := append([]any(nil), have...)
	for _, h := range add {
		key := fmt.Sprint(asObject(h)["command"])
		if !seen[key] {
			seen[key] = true
			out = append(out, h)
		}
	}
	return out
}
