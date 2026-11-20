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
// is serving the session.
func (s *session) onStdinLine(line []byte) {
	if s.phase != phaseRunning {
		s.pending = append(s.pending, line)
		return
	}
	s.forwardIn(line)
}

// forwardIn writes line to the child, remembering the in-flight user message
// and state-setting control requests.
func (s *session) forwardIn(line []byte) {
	if m, ok := parse(line); ok {
		switch {
		case m.Type == typeUser:
			s.inFlight = line
			s.lastUser = line
		case m.Type == typeControlRequest && isReplayable(m.requestSubtype()):
			s.replay[m.requestSubtype()] = line
		}
	}
	s.child.write(line)
}

// onStdinEOF: the SDK closed stdin. A running child gets EOF too and its
// exit ends the proxy; while waiting for an account, the proxy exits now.
func (s *session) onStdinEOF() (int, bool) {
	s.stdinClosed = true
	switch s.phase {
	case phaseRunning:
		s.child.closeStdin()
	case phaseWaiting:
		if s.inFlight == nil {
			return 0, true
		}
		s.logf("SDK closed stdin while waiting for a free account — giving up the turn")
		return exitAbandoned, true
	}
	return 0, false
}

// replayState sends a new child the remembered control requests (their
// answers are swallowed), the resume prompt when a turn was cut off, then
// the lines queued meanwhile.
func (s *session) replayState(resumeNote string) {
	for _, sub := range replayable {
		line, ok := s.replay[sub]
		if !ok {
			continue
		}
		if m, ok := parse(line); ok && m.RequestID != "" {
			s.swallow[m.RequestID] = true
		}
		s.child.write(line)
	}
	if s.inFlight != nil {
		s.child.write(resumeLine(s.lastUser, s.sessionID, resumeNote))
		if hasFlag(s.args, flagReplayUsers) {
			s.swallowEcho++
		}
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
