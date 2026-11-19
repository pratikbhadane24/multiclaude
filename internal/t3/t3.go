// Package t3 syncs ccm accounts into T3 Code's settings.json providerInstances.
package t3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"multiclaude/internal/paths"
	"multiclaude/internal/registry"
)

const (
	keyInstances  = "providerInstances"
	keyDriver     = "driver"
	keyEnabled    = "enabled"
	keyConfig     = "config"
	keyBinaryPath = "binaryPath"
	keyHomePath   = "homePath"
	driverClaude  = "claudeAgent"
	idPrefix      = "claude-"
	backupSuffix  = ".ccm-backup-"
	indent        = "  "
	defaultMode   = os.FileMode(0o600)
	dirMode       = os.FileMode(0o700)
)

// object is a JSON object that remembers key order and keeps values raw.
type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseObject(raw []byte) (object, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return object{vals: map[string]json.RawMessage{}}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return object{}, fmt.Errorf("expected JSON object")
	}
	out := object{vals: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return object{}, err
		}
		key, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return object{}, err
		}
		if _, dup := out.vals[key]; !dup {
			out.keys = append(out.keys, key)
		}
		out.vals[key] = val
	}
	if _, err := dec.Token(); err != nil {
		return object{}, err
	}
	return out, nil
}

// with returns a copy of o with key set to val (appended if new).
func (o object) with(key string, val json.RawMessage) object {
	out := object{keys: append([]string(nil), o.keys...), vals: make(map[string]json.RawMessage, len(o.vals)+1)}
	for k, v := range o.vals {
		out.vals[k] = v
	}
	if _, ok := out.vals[key]; !ok {
		out.keys = append(out.keys, key)
	}
	out.vals[key] = val
	return out
}

func (o object) has(key string) bool { _, ok := o.vals[key]; return ok }

func (o object) raw() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

func (o object) str(key string) string {
	var s string
	_ = json.Unmarshal(o.vals[key], &s)
	return s
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func cleanDir(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// instanceID is the T3 instance id for an account.
func instanceID(a registry.Account) string { return idPrefix + a.Name }

func newInstance(a registry.Account, shimPath string) json.RawMessage {
	cfg := object{vals: map[string]json.RawMessage{}}.
		with(keyBinaryPath, mustMarshal(shimPath)).
		with(keyHomePath, mustMarshal(a.Dir))
	return object{vals: map[string]json.RawMessage{}}.
		with(keyDriver, mustMarshal(driverClaude)).
		with(keyEnabled, mustMarshal(true)).
		with(keyConfig, cfg.raw()).raw()
}

// updateInstance points an existing claudeAgent instance at the shim.
// changed is false when nothing needed to be modified.
func updateInstance(inst object, shimPath string) (json.RawMessage, bool, error) {
	cfg, err := parseObject(inst.vals[keyConfig])
	if err != nil {
		return nil, false, fmt.Errorf("parse instance config: %w", err)
	}
	changed := false
	if cfg.str(keyBinaryPath) != shimPath {
		cfg = cfg.with(keyBinaryPath, mustMarshal(shimPath))
		changed = true
	}
	out := inst
	if changed {
		out = out.with(keyConfig, cfg.raw())
	}
	if !inst.has(keyEnabled) {
		out = out.with(keyEnabled, mustMarshal(true))
		changed = true
	}
	return out.raw(), changed, nil
}

// matchAccount finds the account a claudeAgent instance belongs to.
func matchAccount(inst object, accts []registry.Account) (registry.Account, bool) {
	if inst.str(keyDriver) != driverClaude {
		return registry.Account{}, false
	}
	cfg, err := parseObject(inst.vals[keyConfig])
	if err != nil {
		return registry.Account{}, false
	}
	home := cleanDir(cfg.str(keyHomePath))
	for _, a := range accts {
		if cleanDir(a.Dir) == home {
			return a, true
		}
	}
	return registry.Account{}, false
}

// Merge returns settings with a claudeAgent instance for every account.
// Existing matching instances are updated in place; unrelated keys keep their
// raw bytes. When nothing changes the input is returned unchanged.
func Merge(settings []byte, accts []registry.Account, shimPath string) (out []byte, added, updated []string, err error) {
	top, err := parseObject(settings)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse T3 settings: %w", err)
	}
	insts, err := parseObject(top.vals[keyInstances])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", keyInstances, err)
	}
	insts, updated, err = updateExisting(insts, accts, shimPath)
	if err != nil {
		return nil, nil, nil, err
	}
	matched := matchedNames(insts, accts)
	for _, a := range accts {
		if matched[a.Name] {
			continue
		}
		id := instanceID(a)
		if insts.has(id) {
			return nil, nil, nil, fmt.Errorf("instance id %q already exists with a different homePath", id)
		}
		insts = insts.with(id, newInstance(a, shimPath))
		added = append(added, id)
	}
	if len(added)+len(updated) == 0 {
		return settings, nil, nil, nil
	}
	return render(top.with(keyInstances, insts.raw())), added, updated, nil
}

func updateExisting(insts object, accts []registry.Account, shimPath string) (object, []string, error) {
	var updated []string
	for _, id := range insts.keys {
		inst, err := parseObject(insts.vals[id])
		if err != nil {
			continue // not an object; leave untouched
		}
		if _, ok := matchAccount(inst, accts); !ok {
			continue
		}
		raw, changed, err := updateInstance(inst, shimPath)
		if err != nil {
			return object{}, nil, fmt.Errorf("instance %q: %w", id, err)
		}
		if changed {
			insts = insts.with(id, raw)
			updated = append(updated, id)
		}
	}
	return insts, updated, nil
}

func matchedNames(insts object, accts []registry.Account) map[string]bool {
	out := map[string]bool{}
	for _, id := range insts.keys {
		inst, err := parseObject(insts.vals[id])
		if err != nil {
			continue
		}
		if a, ok := matchAccount(inst, accts); ok {
			out[a.Name] = true
		}
	}
	return out
}

func render(top object) []byte {
	var b bytes.Buffer
	if err := json.Indent(&b, top.raw(), "", indent); err != nil {
		return append(top.raw(), '\n')
	}
	b.WriteByte('\n')
	return b.Bytes()
}

// Sync merges accts into the T3 settings file. It writes nothing when no
// change is needed; otherwise it backs up the original (if any) and writes
// atomically, preserving the file mode.
func Sync(p paths.Paths, accts []registry.Account, now time.Time) (added, updated []string, backupPath string, err error) {
	orig, mode, existed, err := readSettings(p.T3Settings)
	if err != nil {
		return nil, nil, "", err
	}
	out, added, updated, err := Merge(orig, accts, p.ShimPath)
	if err != nil {
		return nil, nil, "", err
	}
	if len(added)+len(updated) == 0 {
		return nil, nil, "", nil
	}
	if existed {
		backupPath = fmt.Sprintf("%s%s%d", p.T3Settings, backupSuffix, now.Unix())
		if err := os.WriteFile(backupPath, orig, mode); err != nil {
			return nil, nil, "", fmt.Errorf("write backup %s: %w", backupPath, err)
		}
	}
	if err := atomicWrite(p.T3Settings, out, mode); err != nil {
		return nil, nil, "", err
	}
	return added, updated, backupPath, nil
}

func readSettings(path string) ([]byte, os.FileMode, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []byte("{}"), defaultMode, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("read %s: %w", path, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("stat %s: %w", path, err)
	}
	return data, fi.Mode().Perm(), true, nil
}

func atomicWrite(dest string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, strings.TrimSuffix(filepath.Base(dest), ".json")+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmp.Name(), err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("rename to %s: %w", dest, err)
	}
	return nil
}
