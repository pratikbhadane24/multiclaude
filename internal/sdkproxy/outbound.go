package sdkproxy

// onChildLine handles one line from the current child: limit signals are
// withheld and acted on, bookkeeping lines for replayed requests are
// dropped, everything else goes to the SDK verbatim.
func (s *session) onChildLine(line []byte) {
	if s.phase != phaseRunning {
		s.keepLeftover(line) // the limited child's last words
		return
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
	case s.armed && m.limitAllowed():
		s.disarm() // requests go through again
		s.emit(line)
	case m.Type == typeResult:
		s.passThrough(m, line)
		if s.armed {
			s.onLimit(nil, false) // the turn finished: switch before the next one
		}
	default:
		if m.Type == typeAssistant && m.errorKind() != "" {
			s.disarm() // the turn failed for another reason (network, billing, auth)
		}
		s.emit(line)
	}
}

// disarm forgets a pending limit and forwards the lines held for it, so
// they never surface later out of order.
func (s *session) disarm() {
	held := s.held
	s.armed, s.armInfo, s.held = false, rateLimitInfo{}, nil
	for _, l := range held {
		s.emit(l)
	}
}

// keepLeftover keeps the result a cut-off child prints while it is being
// stopped, so an interrupt during the wait can end the turn with it.
func (s *session) keepLeftover(line []byte) {
	if s.phase != phaseStopping || !s.cutOff || containsResult(s.withheld) {
		return
	}
	if m, ok := parse(line); ok && m.Type == typeResult {
		s.withheld = append(append([][]byte(nil), s.withheld...), line)
	}
}

// passThrough forwards line; a result ends the oldest outstanding turn, and
// a successful one starts a new switch chain.
func (s *session) passThrough(m message, line []byte) {
	if m.Type == typeResult {
		s.popTurn()
		if !m.IsError {
			s.switches, s.gaveUp = 0, false
		}
	}
	s.emit(line)
}

// dropLine tracks the session id and reports whether m is an artefact of a
// replacement child the SDK must not see: its first system/init for the
// same session, an answer to a replayed control request the SDK already
// had, or the echo of the resume prompt. Other answers settle their
// request.
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
		s.ctrl.remove(id)
	}
	if m.Type == typeUser && m.IsReplay && s.swallowEcho > 0 {
		s.swallowEcho--
		return true
	}
	return false
}
