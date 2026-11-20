package sdkproxy

import (
	"syscall"
	"time"

	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

// accounts loads the registry; on failure only the current account is known.
func (s *session) accounts() []registry.Account {
	if s.d.Accounts != nil {
		list, err := s.d.Accounts()
		if err == nil {
			return list
		}
		s.logf("cannot read accounts: %v", err)
	}
	if s.acctKnown {
		return []registry.Account{s.acct}
	}
	return nil
}

func (s *session) cache() status.Cache {
	if s.d.ReadCache == nil {
		return status.Cache{}
	}
	c, err := s.d.ReadCache()
	if err != nil {
		return status.Cache{} // missing or unreadable: every account is unknown
	}
	return c
}

// chooseStart picks the starting account: the env's, unless the cache says
// it is limited and another account is free.
func (s *session) chooseStart() {
	accts := s.accounts()
	s.acct, s.acctKnown = currentAccount(s.env, s.d.Paths, accts)
	if !s.acctKnown {
		return
	}
	now, cache := s.d.Clock.Now(), s.cache()
	for _, e := range cache.Accounts {
		if e.Name != s.acct.Name {
			continue
		}
		until, limited := status.EntryLimitedUntil(e, now)
		if !limited {
			return
		}
		hold := now.Add(defaultLimitHold)
		if until != nil {
			hold = *until
		}
		choice := status.PickSwitch(accts, cache, map[string]time.Time{s.acct.Name: hold}, now)
		if choice.Found {
			s.logf("%s is limited — starting on %s", s.acct.Name, choice.Account.Name)
			s.acct, s.env = choice.Account, envFor(s.baseEnv, choice.Account)
		}
		return
	}
}

// start launches a child as the current account. A replacement resumes the
// session and gets the remembered state replayed.
func (s *session) start(replace bool, note string) (int, bool) {
	argv := s.args
	if replace && s.sessionID != "" {
		argv = ResumeArgs(s.args, s.sessionID)
	}
	if s.acctKnown && !s.acct.IsMain() && s.d.Prepare != nil {
		s.d.Prepare(s.acct.Dir)
	}
	c, err := s.d.Start(s.d.Paths.RealClaude, append([]string{claudeArgv0}, argv...), s.env)
	if err != nil {
		s.logf("cannot start claude: %v", err)
		return exitCannotStart, true
	}
	s.gen++
	s.child, s.phase = s.attach(c, s.gen), phaseRunning
	if replace {
		s.suppressInit = true
		s.replayState(note)
	}
	return 0, false
}

func (s *session) onChildExit(code int) (int, bool) {
	if s.phase == phaseStopping {
		s.child = nil
		return s.resume()
	}
	return code, true
}

// onLimit reacts to a usage limit: record it, then stop the child so the
// session can continue elsewhere. limitLine is the withheld line that
// reported it (nil when the turn already finished normally).
func (s *session) onLimit(limitLine []byte, cutOff bool) {
	now := s.d.Clock.Now()
	lim := limitFrom(s.armInfo, now)
	withheld := append(s.held, limitLine)
	s.armed, s.armInfo, s.held = false, rateLimitInfo{}, nil
	if s.acctKnown {
		s.avoid[s.acct.Name] = lim.ResetsAt.Add(status.ResetGrace)
		if err := s.d.MarkLimited(s.acct.Name, lim); err != nil {
			s.logf("cannot record %s as limited: %v", s.acct.Name, err)
		}
	}
	if s.switches >= maxSwitches {
		s.logf("%s limited — %d switches already, not switching again", s.accountName(), maxSwitches)
		s.gaveUp = true
		for _, l := range withheld {
			if l != nil {
				s.emit(l)
			}
		}
		return
	}
	if !cutOff {
		s.inFlight = nil
	}
	s.limitedFrom = s.accountName()
	s.stop()
}

// stop terminates the current child: EOF on stdin, SIGTERM, then SIGKILL
// after killDelay. Its exit event resumes the session.
func (s *session) stop() {
	r := s.child
	s.phase = phaseStopping
	r.closeStdin()
	go func() {
		_ = r.child.Signal(syscall.SIGTERM)
		select {
		case <-r.done:
		case <-s.d.Clock.After(killDelay):
			_ = r.child.Signal(syscall.SIGKILL)
		}
	}()
}

// resume continues the session on the best free account, or waits.
func (s *session) resume() (int, bool) {
	if s.stdinClosed && s.inFlight == nil && len(s.pending) == 0 {
		return 0, true
	}
	now := s.d.Clock.Now()
	choice := status.PickSwitch(s.accounts(), s.cache(), s.avoid, now)
	if !choice.Found {
		s.wait(choice.WaitUntil, now)
		return 0, false
	}
	if gap := s.lastSwitch.Add(minSwitchGap).Sub(now); s.switches > 0 && gap > 0 {
		s.phase, s.timer = phaseWaiting, s.d.Clock.After(gap)
		return 0, false
	}
	return s.switchTo(choice.Account, now)
}

func (s *session) wait(until *time.Time, now time.Time) {
	d := pollInterval
	if until != nil && until.Sub(now) > 0 && until.Sub(now) < d {
		d = until.Sub(now)
	}
	if !s.waitLogged {
		s.waitLogged = true
		if until != nil {
			s.logf("%s limited and no account is free — waiting until %s", s.limitedFrom, until.Local().Format("15:04"))
		} else {
			s.logf("%s limited and no account is free — waiting", s.limitedFrom)
		}
	}
	s.phase, s.timer = phaseWaiting, s.d.Clock.After(d)
}

func (s *session) onTimer() (int, bool) {
	if s.phase != phaseWaiting {
		return 0, false
	}
	return s.resume()
}

func (s *session) switchTo(a registry.Account, now time.Time) (int, bool) {
	s.switches++
	s.lastSwitch, s.waitLogged = now, false
	from := s.limitedFrom
	s.logf("%s limited — continuing on %s", from, a.Name)
	s.acct, s.acctKnown, s.env = a, true, envFor(s.baseEnv, a)
	return s.start(true, resumeNote(s.d.ResumePrompt, from, a.Name))
}
