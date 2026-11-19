package t3

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"multiclaude/internal/registry"
)

const keyLaunchArgs = "launchArgs"

// LaunchArgs drives config.launchArgs handling for ccm-managed instances.
type LaunchArgs struct {
	// Desired is the shell-joined args ccm wants, by account name ("" = none).
	Desired map[string]string
	// Written is the last value ccm wrote, by instance id.
	Written map[string]string
}

// LaunchResult reports the new tracking state and user-facing warnings.
type LaunchResult struct {
	Written  map[string]string
	Warnings []string
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}

// without returns a copy of o with key removed.
func (o object) without(key string) object {
	out := object{vals: make(map[string]json.RawMessage, len(o.vals))}
	for _, k := range o.keys {
		if k != key {
			out.keys = append(out.keys, k)
			out.vals[k] = o.vals[k]
		}
	}
	return out
}

// applyLaunchArgs sets, updates or removes launchArgs on every matched
// instance. A value that is neither empty nor the one ccm last wrote is the
// user's and is kept (with a warning when ccm wants something else).
func applyLaunchArgs(insts object, accts []registry.Account, la LaunchArgs) (object, []string, LaunchResult, error) {
	res := LaunchResult{Written: cloneMap(la.Written)}
	var changed []string
	for _, id := range insts.keys {
		inst, err := parseObject(insts.vals[id])
		if err != nil {
			continue
		}
		acct, ok := matchAccount(inst, accts)
		if !ok {
			continue
		}
		next, did, err := applyOne(inst, id, la.Desired[acct.Name], &res)
		if err != nil {
			return object{}, nil, res, fmt.Errorf("instance %q: %w", id, err)
		}
		if did {
			insts = insts.with(id, next)
			changed = append(changed, id)
		}
	}
	return insts, changed, res, nil
}

// applyOne handles one instance; res.Written is updated in place (it is a
// private copy owned by the caller of applyLaunchArgs).
func applyOne(inst object, id, want string, res *LaunchResult) (json.RawMessage, bool, error) {
	cfg, err := parseObject(inst.vals[keyConfig])
	if err != nil {
		return nil, false, fmt.Errorf("parse instance config: %w", err)
	}
	cur := cfg.str(keyLaunchArgs)
	last, wrote := res.Written[id]
	ours := cur == "" || (wrote && cur == last)
	switch {
	case want != "" && cur == want:
		res.Written[id] = want
		return nil, false, nil
	case want != "" && ours:
		res.Written[id] = want
		return withConfig(inst, cfg.with(keyLaunchArgs, mustMarshal(want))), true, nil
	case want != "":
		res.Warnings = append(res.Warnings, fmt.Sprintf("t3: %s launchArgs edited in T3; leaving it (ccm wants: %s)", id, want))
		return nil, false, nil
	case wrote && cur == last && cfg.has(keyLaunchArgs):
		delete(res.Written, id)
		return withConfig(inst, cfg.without(keyLaunchArgs)), true, nil
	case wrote && cur != last:
		// The user took over (or cleared) the value; stop tracking it.
		delete(res.Written, id)
	}
	return nil, false, nil
}

func withConfig(inst, cfg object) json.RawMessage {
	return inst.with(keyConfig, cfg.raw()).raw()
}

// mergeIDs returns ids from a then b, skipping duplicates and anything in skip.
func mergeIDs(a, b, skip []string) []string {
	out := slices.Clone(a)
	for _, id := range b {
		if !slices.Contains(out, id) && !slices.Contains(skip, id) {
			out = append(out, id)
		}
	}
	return out
}
