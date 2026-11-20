package sdkproxy

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	typeUser            = "user"
	typeSystem          = "system"
	typeResult          = "result"
	typeAssistant       = "assistant"
	typeRateLimit       = "rate_limit_event"
	typeControlRequest  = "control_request"
	typeControlResponse = "control_response"
	subtypeInit         = "init"
	statusRejected      = "rejected"
	errorKindRateLimit  = "rate_limit"
)

// message is the handful of stream-json fields the proxy looks at. Fields
// whose shape varies stay raw and are decoded leniently on demand.
type message struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	IsError   bool            `json:"is_error"`
	IsReplay  bool            `json:"isReplay"`
	RequestID string          `json:"request_id"`
	Result    json.RawMessage `json:"result"`
	Errors    json.RawMessage `json:"errors"`
	Error     json.RawMessage `json:"error"`
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
	Message   json.RawMessage `json:"message"`
	RateLimit json.RawMessage `json:"rate_limit_info"`
}

// rateLimitInfo is Claude Code's rate_limit_info payload.
type rateLimitInfo struct {
	Status         string `json:"status"`
	ResetsAt       int64  `json:"resetsAt"`
	RateLimitType  string `json:"rateLimitType"`
	IsUsingOverage bool   `json:"isUsingOverage"`
}

// parse decodes line; ok is false for anything that is not a JSON object.
func parse(line []byte) (message, bool) {
	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		return message{}, false
	}
	return m, true
}

// requestSubtype is request.subtype of a control_request.
func (m message) requestSubtype() string {
	var r struct {
		Subtype string `json:"subtype"`
	}
	_ = json.Unmarshal(m.Request, &r)
	return r.Subtype
}

// responseRequestID is response.request_id of a control_response.
func (m message) responseRequestID() string {
	var r struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(m.Response, &r)
	return r.RequestID
}

// rateLimit decodes rate_limit_info (zero value when absent or malformed).
func (m message) rateLimit() rateLimitInfo {
	var r rateLimitInfo
	_ = json.Unmarshal(m.RateLimit, &r)
	return r
}

// limitRejected: a rate_limit_event saying requests are now refused (and not
// carried on extra usage). allowed / allowed_warning never count.
func (m message) limitRejected() bool {
	if m.Type != typeRateLimit {
		return false
	}
	r := m.rateLimit()
	return r.Status == statusRejected && !r.IsUsingOverage
}

// limitAllowed: a rate_limit_event saying requests go through again.
func (m message) limitAllowed() bool {
	return m.Type == typeRateLimit && m.rateLimit().Status != statusRejected
}

// limitError reports whether m ends or reports a turn refused for a usage
// limit: an error result, or Claude Code's synthetic assistant message with
// error kind "rate_limit", whose text reads like a limit (or armed is set by
// an earlier rejected rate_limit_event). Network, billing and auth errors
// never match.
func (m message) limitError(armed bool) bool {
	switch {
	case m.Type == typeResult && m.IsError:
		return armed || isLimitText(m.resultText())
	case m.Type == typeAssistant && m.errorKind() == errorKindRateLimit:
		return armed || isLimitText(m.assistantText())
	}
	return false
}

func (m message) errorKind() string {
	var s string
	_ = json.Unmarshal(m.Error, &s)
	return s
}

func (m message) resultText() string {
	var res string
	var errs []string
	_ = json.Unmarshal(m.Result, &res)
	_ = json.Unmarshal(m.Errors, &errs)
	return res + "\n" + strings.Join(errs, "\n")
}

func (m message) assistantText() string {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(m.Message, &msg)
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(msg.Content, &blocks)
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n")
}

// isLimitText matches Claude Code's usage-limit texts, e.g. "You've hit your
// 5-hour limit · resets 1:20am" or "API Error: Request rejected (429) · ...".
func isLimitText(s string) bool {
	l := strings.ToLower(strings.ReplaceAll(s, "’", "'"))
	if strings.Contains(l, "request rejected (429)") {
		return true
	}
	i := strings.Index(l, "you've hit your")
	return i >= 0 && strings.Contains(l[i:], "limit")
}

// Limit is one usage-limit hit: which window and when it resets.
type Limit struct {
	Type     string
	ResetsAt time.Time
}

// defaultLimitHold is how long an account is assumed limited when Claude
// Code did not say when the limit resets.
const defaultLimitHold = time.Hour

// limitFrom builds the Limit for a hit, using info from the latest rejected
// rate_limit_event when there was one.
func limitFrom(info rateLimitInfo, now time.Time) Limit {
	l := Limit{Type: info.RateLimitType, ResetsAt: now.Add(defaultLimitHold)}
	if info.ResetsAt > 0 {
		l.ResetsAt = time.Unix(info.ResetsAt, 0).UTC()
	}
	return l
}
