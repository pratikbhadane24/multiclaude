// Package autoswitch moves an interactive claude session to another account
// when the current one hits its usage limit: it watches the session
// transcript, stops claude, and resumes the same conversation elsewhere.
package autoswitch

import (
	"bytes"
	"encoding/json"
	"strings"
)

// rateLimitError is the transcript "error" value Claude Code writes for a
// usage-limit API error.
const rateLimitError = "rate_limit"

// Limit is one detected usage-limit hit.
type Limit struct {
	// Text is the error message shown to the user.
	Text string
	// Reset is the text after "resets " ("1:20am (Asia/Calcutta)"), or "".
	Reset string
	// Weekly is true for the 7-day limit, false for the 5-hour (or unknown) one.
	Weekly bool
}

// limitPhrases match a usage-limit message when the error field is absent.
var limitPhrases = []string{"you've hit your", "usage limit reached", "5-hour limit reached", "weekly limit"}

// excludePhrases mark transient errors that never mean a usage limit.
var excludePhrases = []string{"overloaded", "529", "econnreset", "network", "connection error"}

// transcriptEntry is the subset of a transcript line DetectLimit reads.
type transcriptEntry struct {
	Type        string `json:"type"`
	IsAPIError  bool   `json:"isApiErrorMessage"`
	IsSidechain bool   `json:"isSidechain"`
	Error       string `json:"error"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// DetectLimit reports whether one transcript JSONL line is a usage-limit API
// error of the main conversation. The "error":"rate_limit" field decides;
// when it is missing, the message text is matched against known phrases.
// Other API errors (server, billing, auth, overloaded) never match.
func DetectLimit(line []byte) (Limit, bool) {
	if !bytes.Contains(line, []byte(`"isApiErrorMessage"`)) && !bytes.Contains(line, []byte(rateLimitError)) {
		return Limit{}, false
	}
	var e transcriptEntry
	if json.Unmarshal(line, &e) != nil || e.IsSidechain || (e.Type != "" && e.Type != "assistant") {
		return Limit{}, false
	}
	if !e.IsAPIError && e.Error == "" {
		return Limit{}, false
	}
	text := contentText(e.Message.Content)
	switch {
	case e.Error == rateLimitError:
	case e.Error == "" && isLimitText(text):
	default:
		return Limit{}, false
	}
	return Limit{Text: text, Reset: resetText(text), Weekly: strings.Contains(strings.ToLower(text), "weekly")}, true
}

func isLimitText(text string) bool {
	lower := strings.ToLower(text)
	for _, p := range excludePhrases {
		if strings.Contains(lower, p) {
			return false
		}
	}
	for _, p := range limitPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// contentText joins the text parts of a message content (string or array).
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// resetText is what follows "resets " in text (trimmed), or "".
func resetText(text string) string {
	_, after, ok := strings.Cut(text, "resets ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(after)
}
