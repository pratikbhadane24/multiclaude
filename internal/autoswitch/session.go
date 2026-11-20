package autoswitch

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Claude flags that choose the conversation.
const (
	flagResume      = "--resume"
	flagResumeShort = "-r"
	flagContinue    = "--continue"
	flagContShort   = "-c"
	flagSessionID   = "--session-id"
	flagForkSession = "--fork-session"
)

// resumeRest is the instruction shared by both resume prompts.
const resumeRest = "Resume the task exactly where you left off: first check what was already done " +
	"(files, commands, todos) so you don't repeat finished steps, then continue."

// ResumePrompt is sent after a switch; its %s verbs are the from and to accounts.
const ResumePrompt = "[ccm] Your previous turn was cut off because the account hit its usage limit. " +
	"Usage is available again (switched from %s to %s). " + resumeRest

// ResetResumePrompt is sent when relaunching after waiting for a reset.
const ResetResumePrompt = "[ccm] Your previous turn was cut off by a usage limit. The limit has now reset. " + resumeRest

// BuildResumePrompt is the prompt for a relaunch from -> to; afterReset
// selects the wait-for-reset wording. A non-empty override is used verbatim.
func BuildResumePrompt(from, to string, afterReset bool, override string) string {
	switch {
	case override != "":
		return override
	case afterReset:
		return ResetResumePrompt
	}
	return fmt.Sprintf(ResumePrompt, from, to)
}

// sessionIDRE matches a Claude session id (a UUID).
var sessionIDRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ErrUnknownSession means the launch args pick a conversation whose id cannot
// be known in advance (bare --resume picker, a search term, --fork-session).
var ErrUnknownSession = errors.New("cannot tell which conversation this launch will use")

// ResolveSession returns launch args that pin a known session id, and that id:
// --resume <id> / --session-id <id> are kept, --continue is replaced by
// --resume <latest()>, and with none of them --session-id <newID()> is added.
// args is not modified.
func ResolveSession(args []string, newID func() string, latest func() (string, error)) ([]string, string, error) {
	for _, a := range args {
		if a == flagForkSession {
			return nil, "", fmt.Errorf("%w (%s)", ErrUnknownSession, flagForkSession)
		}
	}
	for i, a := range args {
		name, value, hasValue := strings.Cut(a, "=")
		switch name {
		case flagResume, flagResumeShort, flagSessionID:
			return pinnedID(args, i, value, hasValue)
		case flagContinue, flagContShort:
			return continueToResume(args, i, latest)
		}
	}
	id := newID()
	out := append(append(make([]string, 0, len(args)+2), args...), flagSessionID, id)
	return out, id, nil
}

// pinnedID reads the id given to the flag at i (inline or as the next arg).
func pinnedID(args []string, i int, value string, hasValue bool) ([]string, string, error) {
	if !hasValue && i+1 < len(args) {
		value = args[i+1]
	}
	if !sessionIDRE.MatchString(value) {
		return nil, "", fmt.Errorf("%w (%s without a session id)", ErrUnknownSession, args[i])
	}
	return append([]string(nil), args...), value, nil
}

// continueToResume swaps the --continue at i for --resume <latest id>.
func continueToResume(args []string, i int, latest func() (string, error)) ([]string, string, error) {
	id, err := latest()
	if err != nil {
		return nil, "", fmt.Errorf("resolve %s: %w", args[i], err)
	}
	out := make([]string, 0, len(args)+1)
	out = append(out, args[:i]...)
	out = append(out, flagResume, id)
	out = append(out, args[i+1:]...)
	return out, id, nil
}

// StripSessionFlags returns args without the flags that choose a
// conversation (and their ids), so a relaunch can add its own --resume.
func StripSessionFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		name, _, hasValue := strings.Cut(args[i], "=")
		switch name {
		case flagContinue, flagContShort, flagForkSession:
			continue
		case flagResume, flagResumeShort, flagSessionID:
			if !hasValue && i+1 < len(args) && sessionIDRE.MatchString(args[i+1]) {
				i++
			}
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// RelaunchArgs is base (the configured args) without session flags, then
// --resume id and the resume prompt as the final positional argument.
func RelaunchArgs(base []string, id, prompt string) []string {
	return append(StripSessionFlags(base), flagResume, id, prompt)
}
