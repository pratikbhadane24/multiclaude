package autoswitch

import (
	"context"
	"fmt"
	"time"

	"multiclaude/internal/registry"
	"multiclaude/internal/status"
)

// afterLimit handles a limit hit on from: it records the limit and returns
// the account and prompt to relaunch with, or ok=false with the exit code.
func (s *supervisor) afterLimit(ctx context.Context, from registry.Account, out outcome) (registry.Account, string, int, bool) {
	if err := s.d.Mark(from, out.limit, s.d.Now()); err != nil {
		fmt.Fprintf(s.d.Err, "ccm: warning: could not record the limit of %s: %v\n", from.Name, err)
	}
	if s.switches >= MaxSwitches {
		fmt.Fprintf(s.d.Err, "ccm: %s hit its limit — auto-switch stopped after %d switches\n", from.Name, MaxSwitches)
		return registry.Account{}, "", out.code, false
	}
	next, ok := s.d.Pick(s.chain, s.d.Now())
	if !ok {
		return s.waitForReset(ctx, out.code)
	}
	if !s.sleep(ctx, s.gapLeft()) {
		return registry.Account{}, "", exitInterrupted, false
	}
	fmt.Fprintf(s.d.Err, "ccm: %s hit its limit — continuing on %s\n", from.Name, next.Name)
	s.chain = append(s.chain, next.Name)
	s.switches, s.lastSwitch = s.switches+1, s.d.Now()
	return next, BuildResumePrompt(from.Name, next.Name, false, s.d.PromptOverride), 0, true
}

// gapLeft is how long to wait so two switches are MinSwitchGap apart.
func (s *supervisor) gapLeft() time.Duration {
	if s.switches == 0 {
		return 0 // first switch of a chain (or right after a reset wait)
	}
	return max(0, MinSwitchGap-s.d.Now().Sub(s.lastSwitch))
}

// waitForReset waits for the soonest known reset (+ResetSlack) and starts a
// new chain on that account; without a known reset it gives up with code.
func (s *supervisor) waitForReset(ctx context.Context, code int) (registry.Account, string, int, bool) {
	now := s.d.Now()
	acct, at, ok := s.d.NextReset(now)
	if !ok {
		fmt.Fprintln(s.d.Err, "ccm: all accounts are limited — next reset unknown")
		return registry.Account{}, "", code, false
	}
	wait := at.Add(ResetSlack).Sub(now)
	fmt.Fprintf(s.d.Err, "ccm: all accounts are limited — next reset %s on %s (in %s); waiting, Ctrl-C to stop\n",
		at.Local().Format(resetTimeLayout), acct.Name, status.FormatDuration(wait))
	if !s.sleep(ctx, wait) {
		fmt.Fprintln(s.d.Err, "ccm: stopped waiting")
		return registry.Account{}, "", exitInterrupted, false
	}
	fmt.Fprintf(s.d.Err, "ccm: %s has reset — continuing on %s\n", acct.Name, acct.Name)
	s.chain, s.switches, s.lastSwitch = []string{acct.Name}, 0, s.d.Now()
	return acct, BuildResumePrompt(acct.Name, acct.Name, true, s.d.PromptOverride), 0, true
}

// sleep waits d (nothing when d <= 0); false means ctx ended first.
func (s *supervisor) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-s.d.After(d):
		return true
	}
}
