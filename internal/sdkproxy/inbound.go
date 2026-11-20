package sdkproxy

// replayable are the SDK control requests that set up a child's state. The
// latest of each is replayed, in this order, to every replacement child.
var replayable = []string{"initialize", "set_permission_mode", "set_model", "set_max_thinking_tokens"}

func isReplayable(subtype string) bool {
	for _, r := range replayable {
		if r == subtype {
			return true
		}
	}
	return false
}

// onStdinLine forwards an SDK line to the child, or queues it while no child
// is serving the session. While no child runs an interrupt is answered at
// once, and a line arriving when idle starts a child.
func (s *session) onStdinLine(line []byte) (int, bool) {
	if s.phase == phaseRunning {
		s.forwardIn(line)
		return 0, false
	}
	if m, ok := isInterrupt(line); ok && s.phase != phaseStopping {
		return s.interruptWaiting(m)
	}
	s.pending = append(s.pending, line)
	if s.phase == phaseIdle {
		return s.resume()
	}
	return 0, false
}

// forwardIn writes line to the child, remembering user messages until their
// result and control requests until their answer.
func (s *session) forwardIn(line []byte) {
	m, ok := parse(line)
	switch {
	case !ok:
	case m.Type == typeUser:
		s.lastUser = line
		if s.gaveUp {
			// A new turn: giving up held for the previous one only.
			s.gaveUp, s.switches = false, 0
		}
		s.outstanding = append(s.outstanding, line)
	case m.Type == typeControlRequest:
		if isReplayable(m.requestSubtype()) {
			s.replay[m.requestSubtype()] = line
		}
		if m.RequestID != "" {
			s.ctrl.add(m.RequestID, line)
		}
	}
	s.child.write(line)
}

// popTurn ends the oldest outstanding turn.
func (s *session) popTurn() {
	if len(s.outstanding) > 0 {
		s.outstanding = s.outstanding[1:]
	}
}

// hasQueuedTurns reports user messages not yet answered: sent to a child
// or waiting to be sent.
func (s *session) hasQueuedTurns() bool {
	if len(s.outstanding) > 0 {
		return true
	}
	for _, l := range s.pending {
		if m, ok := parse(l); ok && m.Type == typeUser {
			return true
		}
	}
	return false
}

// onStdinEOF: the SDK closed stdin. A running child gets EOF too and its
// exit ends the proxy. Without a child, unanswered messages are still
// delivered once an account is free (stdin is closed after them);
// otherwise the proxy exits now.
func (s *session) onStdinEOF() (int, bool) {
	s.stdinClosed = true
	switch s.phase {
	case phaseRunning:
		s.child.closeStdin()
	case phaseWaiting, phaseIdle:
		if !s.hasQueuedTurns() {
			return 0, true
		}
		s.logf("SDK closed stdin — its messages are delivered once an account is free")
	}
	return 0, false
}

// replayState brings a new child up to date: the remembered state requests,
// the resume prompt for a cut-off turn, every user message still
// unanswered, the other unanswered control requests, then the lines
// queued meanwhile.
func (s *session) replayState(resumeNote string) {
	sent := s.replayControls()
	s.resendTurns(resumeNote)
	for _, l := range s.ctrl.unanswered(sent) {
		s.child.write(l)
	}
	queued := s.pending
	s.pending = nil
	for _, l := range queued {
		s.forwardIn(l)
	}
	if s.stdinClosed {
		s.child.closeStdin()
	}
}

// replayControls sends the latest state request of each kind. The answer
// is swallowed when the SDK already had one, and let through when it is
// still waiting. It returns the ids it sent.
func (s *session) replayControls() map[string]bool {
	sent := map[string]bool{}
	for _, sub := range replayable {
		line, ok := s.replay[sub]
		if !ok {
			continue
		}
		if m, ok := parse(line); ok && m.RequestID != "" {
			sent[m.RequestID] = true
			if !s.ctrl.has(m.RequestID) {
				s.swallow[m.RequestID] = true
			}
		}
		s.child.write(line)
	}
	return sent
}

// resendTurns sends the unanswered user messages again: a cut-off turn as
// the resume prompt (the transcript already holds its message), the later
// ones verbatim, in order.
func (s *session) resendTurns(resumeNote string) {
	turns := s.outstanding
	s.outstanding = nil
	if s.cutOff && len(turns) > 0 {
		prompt := resumeLine(s.lastUser, s.sessionID, resumeNote)
		s.outstanding = append(s.outstanding, prompt)
		s.child.write(prompt)
		if hasFlag(s.args, flagReplayUsers) {
			s.swallowEcho++
		}
		turns = turns[1:]
	}
	s.cutOff, s.withheld = false, nil
	for _, l := range turns {
		s.outstanding = append(s.outstanding, l)
		s.child.write(l)
	}
}
