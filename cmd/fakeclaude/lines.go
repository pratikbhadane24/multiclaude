package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// limitText is the message real claude prints when the 5-hour window is full.
const limitText = "You've hit your 5-hour limit · resets 1:20am (Asia/Calcutta)"

// newUUID returns a random RFC 4122 version 4 UUID.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// slug converts a working directory into claude's project directory name.
func slug(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
}

// transcriptPath is <configDir or home/.claude>/projects/<slug(cwd)>/<id>.jsonl.
func transcriptPath(configDir, home, cwd, id string) string {
	base := configDir
	if base == "" {
		base = filepath.Join(home, ".claude")
	}
	return filepath.Join(base, "projects", slug(cwd), id+".jsonl")
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func textMessage(role, text string) map[string]any {
	return map[string]any{
		"role":    role,
		"content": []any{map[string]any{"type": "text", "text": text}},
	}
}

// limitLine is the transcript line claude appends when rate limited.
func limitLine(id, cwd string, now time.Time) map[string]any {
	return map[string]any{
		"type": "assistant", "isApiErrorMessage": true, "error": "rate_limit",
		"isSidechain": false, "sessionId": id, "session_id": id, "cwd": cwd,
		"uuid": newUUID(), "parentUuid": nil, "timestamp": stamp(now),
		"message": textMessage("assistant", limitText),
	}
}

func transcriptLine(typ, text, id, cwd string, now time.Time) map[string]any {
	return map[string]any{
		"type": typ, "isSidechain": false, "sessionId": id, "session_id": id,
		"cwd": cwd, "uuid": newUUID(), "parentUuid": nil, "timestamp": stamp(now),
		"message": textMessage(typ, text),
	}
}

func userLine(text, id, cwd string, now time.Time) map[string]any {
	return transcriptLine("user", text, id, cwd, now)
}

func assistantLine(text, id, cwd string, now time.Time) map[string]any {
	return transcriptLine("assistant", text, id, cwd, now)
}

func marshalLine(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only maps of plain values are marshaled here
	}
	return append(b, '\n')
}
