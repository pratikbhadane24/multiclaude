package cli

import (
	"regexp"
	"testing"
)

var uuidV4RE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewSessionIDIsUniqueUUIDv4(t *testing.T) {
	a, b := newSessionID(), newSessionID()

	if !uuidV4RE.MatchString(a) || a == b {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestDashDepsGeneratesSessionIDs(t *testing.T) {
	e := newCLI(t, "")

	deps := e.app.dashDeps()

	if deps.NewSessionID == nil || !uuidV4RE.MatchString(deps.NewSessionID()) {
		t.Fatal("dash deps must supply session ids")
	}
}
