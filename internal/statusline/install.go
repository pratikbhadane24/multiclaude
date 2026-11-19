package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	statusLineKey = "statusLine"
	commandKey    = "command"
	commandType   = "command"
	noneText      = "(none)"
	ccmBase       = "ccm"
	subcommand    = "statusline"
	wrapFlag      = "--wrap"
)

// Edit is a planned change to settings.json. Content holds the full new file
// (nil when NoOp); Before/After are the statusLine values for display.
type Edit struct {
	Before, After string
	Warn          string
	NoOp          bool
	Content       []byte
}

// span locates one top-level member inside a JSON object's bytes.
type span struct {
	key                        string
	keyStart, valStart, valEnd int
}

// parseSpans returns member byte offsets of the JSON object in data and the
// index of its closing brace. Everything outside the offsets stays untouched.
func parseSpans(data []byte) (spans []span, closeIdx int, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, terr := dec.Token(); terr != nil || tok != json.Delim('{') {
		return nil, 0, errors.New("not a JSON object")
	}
	for dec.More() {
		tok, terr := dec.Token()
		if terr != nil {
			return nil, 0, terr
		}
		key, _ := tok.(string)
		keyEnd := int(dec.InputOffset())
		var raw json.RawMessage
		if terr := dec.Decode(&raw); terr != nil {
			return nil, 0, terr
		}
		valEnd := int(dec.InputOffset())
		quoted, _ := json.Marshal(key)
		keyStart := keyEnd - len(quoted)
		if keyStart < 0 || !bytes.Equal(data[keyStart:keyEnd], quoted) {
			keyStart = -1
		}
		spans = append(spans, span{key, keyStart, valEnd - len(raw), valEnd})
	}
	if _, terr := dec.Token(); terr != nil {
		return nil, 0, terr
	}
	closeIdx = int(dec.InputOffset()) - 1
	if _, terr := dec.Token(); terr != io.EOF {
		return nil, 0, errors.New("trailing data after JSON object")
	}
	return spans, closeIdx, nil
}

func lastSpan(spans []span, key string) (int, bool) {
	for i := len(spans) - 1; i >= 0; i-- {
		if spans[i].key == key {
			return i, true
		}
	}
	return -1, false
}

func splice(data []byte, from, to int, repl string) []byte {
	out := make([]byte, 0, len(data)-(to-from)+len(repl))
	out = append(out, data[:from]...)
	out = append(out, repl...)
	return append(out, data[to:]...)
}

func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

func freshStatusLine(command string) string {
	return `{"type":"` + commandType + `","command":` + jsonString(command) + `}`
}

// memberSeparator is what to write between the last member and a new one,
// matching the file's indentation when the members are on their own lines.
func memberSeparator(data []byte, last span) string {
	if last.keyStart > 0 {
		nl := bytes.LastIndexByte(data[:last.keyStart], '\n')
		if nl >= 0 && len(bytes.TrimSpace(data[nl+1:last.keyStart])) == 0 {
			return "," + string(data[nl:last.keyStart])
		}
	}
	return ", "
}

// addStatusLine appends a statusLine member to the object in data.
func addStatusLine(data []byte, spans []span, closeIdx int, val string) []byte {
	member := jsonString(statusLineKey) + ": " + val
	if len(spans) == 0 {
		open := bytes.IndexByte(data, '{')
		return splice(data, open+1, closeIdx, "\n  "+member+"\n")
	}
	last := spans[len(spans)-1]
	return splice(data, last.valEnd, last.valEnd, memberSeparator(data, last)[:]+member)
}

// removeSpan deletes span i together with its separating comma.
func removeSpan(data []byte, spans []span, closeIdx, i int) ([]byte, error) {
	s := spans[i]
	switch {
	case i > 0:
		return splice(data, spans[i-1].valEnd, s.valEnd, ""), nil
	case len(spans) > 1 && s.keyStart >= 0:
		return splice(data, s.keyStart, spans[1].keyStart, ""), nil
	case len(spans) == 1:
		open := bytes.IndexByte(data, '{')
		return splice(data, open+1, closeIdx, ""), nil
	}
	return nil, errors.New("cannot remove statusLine: unsupported key formatting")
}

type statusLineInfo struct {
	raw     []byte
	isCmd   bool
	command string
}

func inspectStatusLine(data []byte, s span) statusLineInfo {
	raw := data[s.valStart:s.valEnd]
	var v struct {
		Type    string `json:"type"`
		Command any    `json:"command"`
	}
	info := statusLineInfo{raw: raw}
	if err := json.Unmarshal(raw, &v); err != nil || v.Type != commandType {
		return info
	}
	if cmd, ok := v.Command.(string); ok {
		info.isCmd, info.command = true, cmd
	}
	return info
}

// shellQuote single-quotes s unless it is made only of safe characters.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parseWord reads one shell word (bare, single-quoted and backslash parts)
// from the start of s and returns it with the remaining text.
func parseWord(s string) (word, rest string, ok bool) {
	var b strings.Builder
	i := 0
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		switch s[i] {
		case '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return "", "", false
			}
			b.WriteString(s[i+1 : i+1+end])
			i += end + 2
		case '\\':
			if i+1 >= len(s) {
				return "", "", false
			}
			b.WriteByte(s[i+1])
			i += 2
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String(), s[i:], true
}

// ccmWrapped reports whether command is a ccm statusline command and returns
// the wrapped command ("" when ccm was installed without one).
func ccmWrapped(command string) (wrap string, ok bool) {
	exe, rest, parsed := parseWord(strings.TrimSpace(command))
	if !parsed || filepath.Base(exe) != ccmBase {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == subcommand {
		return "", true
	}
	arg, found := strings.CutPrefix(rest, subcommand+" "+wrapFlag+" ")
	if !found {
		return "", false
	}
	w, tail, ok := parseWord(strings.TrimSpace(arg))
	if !ok || strings.TrimSpace(tail) != "" {
		return "", false
	}
	return w, true
}

func newCommand(exe, wrap string) string {
	cmd := shellQuote(exe) + " " + subcommand
	if wrap != "" {
		cmd += " " + wrapFlag + " " + shellQuote(wrap)
	}
	return cmd
}

func setCommand(data []byte, s span, command string) ([]byte, error) {
	inner := data[s.valStart:s.valEnd]
	spans, _, err := parseSpans(inner)
	if err != nil {
		return nil, err
	}
	i, ok := lastSpan(spans, commandKey)
	if !ok {
		return nil, errors.New("statusLine has no command")
	}
	return splice(inner, spans[i].valStart, spans[i].valEnd, jsonString(command)), nil
}

func parseSettings(settings []byte) ([]byte, []span, int, error) {
	if len(bytes.TrimSpace(settings)) == 0 {
		settings = []byte("{}\n")
	}
	spans, closeIdx, err := parseSpans(settings)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("settings.json: %w", err)
	}
	return settings, spans, closeIdx, nil
}

// PlanInstall computes settings.json with the ccm status line installed,
// wrapping any existing command. settings may be nil (no file yet).
func PlanInstall(settings []byte, exe string) (Edit, error) {
	data, spans, closeIdx, err := parseSettings(settings)
	if err != nil {
		return Edit{}, err
	}
	idx, found := lastSpan(spans, statusLineKey)
	if !found {
		val := freshStatusLine(newCommand(exe, ""))
		return Edit{Before: noneText, After: val, Content: addStatusLine(data, spans, closeIdx, val)}, nil
	}
	info := inspectStatusLine(data, spans[idx])
	before := string(info.raw)
	if _, ours := ccmWrapped(info.command); info.isCmd && ours {
		return Edit{Before: before, After: before, NoOp: true}, nil
	}
	if !info.isCmd || info.command == "" {
		val := freshStatusLine(newCommand(exe, ""))
		e := Edit{Before: before, After: val, Content: splice(data, spans[idx].valStart, spans[idx].valEnd, val)}
		if !info.isCmd {
			e.Warn = "existing statusLine is not a command type and cannot be wrapped; it will be replaced (restore it from the backup)"
		}
		return e, nil
	}
	val, err := setCommand(data, spans[idx], newCommand(exe, info.command))
	if err != nil {
		return Edit{}, err
	}
	return Edit{Before: before, After: string(val), Content: splice(data, spans[idx].valStart, spans[idx].valEnd, string(val))}, nil
}

// PlanUninstall computes settings.json with the ccm status line removed,
// restoring the wrapped command or dropping a statusLine ccm added fresh.
func PlanUninstall(settings []byte) (Edit, error) {
	data, spans, closeIdx, err := parseSettings(settings)
	if err != nil {
		return Edit{}, err
	}
	idx, found := lastSpan(spans, statusLineKey)
	if !found {
		return Edit{Before: noneText, After: noneText, NoOp: true}, nil
	}
	info := inspectStatusLine(data, spans[idx])
	wrap, ours := ccmWrapped(info.command)
	if !info.isCmd || !ours {
		return Edit{Before: string(info.raw), After: string(info.raw), NoOp: true}, nil
	}
	if wrap == "" {
		content, err := removeSpan(data, spans, closeIdx, idx)
		if err != nil {
			return Edit{}, err
		}
		return Edit{Before: string(info.raw), After: noneText, Content: content}, nil
	}
	val, err := setCommand(data, spans[idx], wrap)
	if err != nil {
		return Edit{}, err
	}
	return Edit{Before: string(info.raw), After: string(val), Content: splice(data, spans[idx].valStart, spans[idx].valEnd, string(val))}, nil
}
