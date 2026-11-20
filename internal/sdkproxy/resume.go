package sdkproxy

import (
	"encoding/json"
	"strings"
)

const (
	// ResumeSwitchedPrompt tells the model its turn was cut off and the
	// session moved to another account. {from} and {to} are account names.
	ResumeSwitchedPrompt = "[ccm] Your previous turn was cut off because the account hit its usage limit. " +
		"Usage is available again (switched from {from} to {to}). Resume the task exactly where you left off: " +
		"first check what was already done (files, commands, todos) so you don't repeat finished steps, then continue."
	// ResumeResetPrompt is used when the same account's limit has reset.
	ResumeResetPrompt = "[ccm] Your previous turn was cut off by a usage limit. The limit has now reset. " +
		"Resume the task exactly where you left off: first check what was already done (files, commands, todos) " +
		"so you don't repeat finished steps, then continue."
)

// resumeNote is the text sent to a replacement child whose turn was cut
// off: custom verbatim when set, else the built-in prompt for the case.
func resumeNote(custom, from, to string) string {
	if custom != "" {
		return custom
	}
	if from == to {
		return ResumeResetPrompt
	}
	return strings.NewReplacer("{from}", from, "{to}", to).Replace(ResumeSwitchedPrompt)
}

// resumeLine builds the user message that restarts a cut-off turn. The
// transcript already holds the original message, so it is not resent: the
// last SDK user line keeps its shape (session fields etc.) with its content
// replaced by note, parent_tool_use_id null and its uuid dropped (it would
// duplicate the original's). Without a usable captured line a minimal SDK
// user message is built.
func resumeLine(captured []byte, sessionID, note string) []byte {
	content := []map[string]string{{"type": "text", "text": note}}
	msg, _ := json.Marshal(map[string]any{"role": "user", "content": content})
	var fields map[string]json.RawMessage
	if json.Unmarshal(captured, &fields) != nil || fields == nil {
		sid, _ := json.Marshal(sessionID)
		fields = map[string]json.RawMessage{"type": json.RawMessage(`"user"`), "session_id": sid}
	}
	out := make(map[string]json.RawMessage, len(fields)+2)
	for k, v := range fields {
		if k != "uuid" {
			out[k] = v
		}
	}
	out["message"], out["parent_tool_use_id"] = msg, json.RawMessage("null")
	line, _ := json.Marshal(out)
	return append(line, '\n')
}
