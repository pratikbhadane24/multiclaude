package sdkproxy

// onChildLine handles one line from the current child: limit signals are
// withheld and acted on, bookkeeping lines for replayed requests are
// dropped, everything else goes to the SDK verbatim.
func (s *session) onChildLine(line []byte) {
	if s.phase != phaseRunning {
		return // the limited child's leftovers (its error result etc.)
	}
	m, ok := parse(line)
	if !ok {
		s.emit(line)
		return
	}
	if s.dropLine(m) {
		return
	}
	if s.gaveUp {
		s.passThrough(m, line)
		return
	}
	switch {
	case m.limitRejected():
		s.armed, s.armInfo = true, m.rateLimit()
		s.held = append(s.held, line)
	case m.limitError(s.armed):
		s.onLimit(line, true)
	case m.Type == typeResult:
		s.passThrough(m, line)
		if s.armed {
			s.onLimit(nil, false) // the turn finished: switch before the next one
		}
	default:
		if m.Type == typeAssistant && m.errorKind() != "" {
			s.armed = false // the turn failed for another reason (network, billing, auth)
		}
		s.emit(line)
	}
}

// passThrough forwards line, ending the in-flight turn on a result.
func (s *session) passThrough(m message, line []byte) {
	if m.Type == typeResult {
		s.inFlight = nil
	}
	s.emit(line)
}

// dropLine tracks the session id and reports whether m is an artefact of a
// replacement child the SDK must not see: its first system/init for the
// same session, an answer to a replayed control request, or the echo of the
// resume prompt.
func (s *session) dropLine(m message) bool {
	if m.Type == typeSystem && m.Subtype == subtypeInit && s.suppressInit {
		s.suppressInit = false
		if m.SessionID == s.sessionID {
			return true
		}
	}
	if m.SessionID != "" {
		s.sessionID = m.SessionID
	}
	if m.Type == typeControlResponse {
		id := m.responseRequestID()
		if s.swallow[id] {
			delete(s.swallow, id)
			return true
		}
	}
	if m.Type == typeUser && m.IsReplay && s.swallowEcho > 0 {
		s.swallowEcho--
		return true
	}
	return false
}
