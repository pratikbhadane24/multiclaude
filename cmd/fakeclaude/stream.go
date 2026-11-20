package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	maxStreamLine   = 16 << 20
	initialBufBytes = 64 << 10
	resetsInSeconds = 3600
)

// userText extracts the text of a stream-json user message.
func userText(raw json.RawMessage) string {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type streamIn struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype string `json:"subtype"`
	} `json:"request"`
	Message json.RawMessage `json:"message"`
}

// runStream speaks the stream-json protocol until in closes.
func runStream(in io.Reader, out io.Writer, e env, id string, now func() time.Time, onUser func(string) error) error {
	emit := func(v any) error { _, err := out.Write(marshalLine(v)); return err }
	if err := emit(map[string]any{
		"type": "system", "subtype": "init", "session_id": id, "cwd": e.Cwd,
		"model": "fakeclaude", "tools": []any{}, "uuid": newUUID(),
	}); err != nil {
		return err
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, initialBufBytes), maxStreamLine)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg streamIn
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		var err error
		switch msg.Type {
		case "control_request":
			err = emit(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": msg.RequestID, "response": map[string]any{},
			}})
		case "user":
			text := userText(msg.Message)
			if err = onUser(text); err == nil {
				err = replyToUser(emit, e, id, text, now())
			}
		}
		if err != nil {
			return err
		}
	}
	return sc.Err()
}

func replyToUser(emit func(any) error, e env, id, text string, now time.Time) error {
	if e.Limited {
		if err := emit(map[string]any{
			"type": "rate_limit_event", "session_id": id, "uuid": newUUID(),
			"rate_limit_info": map[string]any{
				"status": "rejected", "resetsAt": now.Unix() + resetsInSeconds,
				"rateLimitType": "five_hour", "utilization": 1.0,
			},
		}); err != nil {
			return err
		}
		return emit(map[string]any{"type": "result", "subtype": "success", "is_error": true,
			"result": limitText, "session_id": id})
	}
	if err := emit(map[string]any{
		"type": "assistant", "session_id": id, "uuid": newUUID(),
		"message": textMessage("assistant", fmt.Sprintf("fakeclaude ok on %s, echo: %s", e.Account, text)),
	}); err != nil {
		return err
	}
	return emit(map[string]any{"type": "result", "subtype": "success", "is_error": false,
		"result": "ok", "session_id": id})
}
