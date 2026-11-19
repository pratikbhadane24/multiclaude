package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	fileMode   = 0o600
	tempSuffix = ".tmp"
	indent     = "  "
)

// member is one top-level key of a JSON object, with its value kept verbatim
// so that keys ccm does not own are written back byte-for-byte.
type member struct {
	key string
	val json.RawMessage
}

// parseObject decodes a JSON object into its members, preserving order and
// the exact bytes of every value.
func parseObject(data []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	members := []member{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		members = append(members, member{key: key, val: val})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON object")
	}
	return members, nil
}

// lookup returns the value of key (the last one wins, as in encoding/json).
func lookup(members []member, key string) (json.RawMessage, bool) {
	for i := len(members) - 1; i >= 0; i-- {
		if members[i].key == key {
			return members[i].val, true
		}
	}
	return nil, false
}

// withMember returns a copy of members with key set to val (appended if new).
func withMember(members []member, key string, val json.RawMessage) []member {
	out := make([]member, 0, len(members)+1)
	found := false
	for _, m := range members {
		if m.key == key {
			if found {
				continue
			}
			m = member{key: key, val: val}
			found = true
		}
		out = append(out, m)
	}
	if !found {
		out = append(out, member{key: key, val: val})
	}
	return out
}

// renderObject writes members as a JSON object, one key per line, values verbatim.
func renderObject(members []member) ([]byte, error) {
	if len(members) == 0 {
		return []byte("{}\n"), nil
	}
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, m := range members {
		key, err := json.Marshal(m.key)
		if err != nil {
			return nil, err
		}
		buf.WriteString(indent)
		buf.Write(key)
		buf.WriteString(": ")
		buf.Write(m.val)
		if i < len(members)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// writeAtomic writes data to a temp file in path's dir (mode 0600) and renames
// it over path, so readers never see a partial file.
func writeAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*"+tempSuffix)
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(fileMode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
