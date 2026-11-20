package sdkproxy

import "encoding/json"

const subtypeInterrupt = "interrupt"

// ctrlTracker remembers the SDK control requests a child has been sent but
// not answered, in the order they were sent.
type ctrlTracker struct {
	ids   []string
	lines map[string][]byte
}

func newCtrlTracker() *ctrlTracker { return &ctrlTracker{lines: map[string][]byte{}} }

func (t *ctrlTracker) add(id string, line []byte) {
	if _, dup := t.lines[id]; !dup {
		t.ids = append(t.ids, id)
	}
	t.lines[id] = line
}

func (t *ctrlTracker) has(id string) bool {
	_, ok := t.lines[id]
	return ok
}

func (t *ctrlTracker) remove(id string) {
	if !t.has(id) {
		return
	}
	delete(t.lines, id)
	kept := make([]string, 0, len(t.ids))
	for _, x := range t.ids {
		if x != id {
			kept = append(kept, x)
		}
	}
	t.ids = kept
}

// unanswered returns the remembered requests, oldest first, skipping skip.
func (t *ctrlTracker) unanswered(skip map[string]bool) [][]byte {
	out := make([][]byte, 0, len(t.ids))
	for _, id := range t.ids {
		if !skip[id] {
			out = append(out, t.lines[id])
		}
	}
	return out
}

// controlSuccess is the control_response the proxy itself gives a request.
func controlSuccess(requestID string) []byte {
	line, _ := json.Marshal(map[string]any{
		"type":     typeControlResponse,
		"response": map[string]any{"subtype": "success", "request_id": requestID},
	})
	return append(line, '\n')
}

// interruptedResult ends a cut-off turn the SDK interrupted while no child
// could run it, when the limited child never printed its own result.
func interruptedResult(sessionID string) []byte {
	line, _ := json.Marshal(map[string]any{
		"type": typeResult, "subtype": "error_during_execution", "is_error": true,
		"result":      "[ccm] interrupted while waiting for an account with usage left",
		"session_id":  sessionID,
		"duration_ms": 0, "duration_api_ms": 0, "num_turns": 0, "total_cost_usd": 0,
		"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
	})
	return append(line, '\n')
}

func isInterrupt(line []byte) (message, bool) {
	m, ok := parse(line)
	return m, ok && m.Type == typeControlRequest && m.requestSubtype() == subtypeInterrupt
}

// interruptWaiting answers an interrupt that arrived while no child runs:
// the cut-off turn ends (its limit lines reach the SDK) and, with nothing
// else to deliver, the proxy stops waiting for an account.
func (s *session) interruptWaiting(m message) (int, bool) {
	s.emit(controlSuccess(m.RequestID))
	if s.cutOff && len(s.outstanding) > 0 {
		s.endCutOffTurn()
	}
	if s.hasQueuedTurns() || len(s.pending) > 0 {
		return 0, false
	}
	s.logf("interrupted — no longer waiting for an account")
	s.phase, s.timer = phaseIdle, nil
	if s.stdinClosed {
		return 0, true
	}
	return 0, false
}

// endCutOffTurn forwards the cut-off turn's withheld lines, adding a result
// when none was captured, and drops the turn.
func (s *session) endCutOffTurn() {
	lines := s.withheld
	if !containsResult(lines) {
		lines = append(append([][]byte(nil), lines...), interruptedResult(s.sessionID))
	}
	s.cutOff, s.withheld = false, nil
	s.emitWithheld(lines)
}

// emitWithheld forwards lines held back from the SDK; each result among
// them ends the oldest outstanding turn.
func (s *session) emitWithheld(lines [][]byte) {
	for _, l := range lines {
		if m, ok := parse(l); ok && m.Type == typeResult {
			s.popTurn()
		}
		s.emit(l)
	}
}

func containsResult(lines [][]byte) bool {
	for _, l := range lines {
		if m, ok := parse(l); ok && m.Type == typeResult {
			return true
		}
	}
	return false
}
