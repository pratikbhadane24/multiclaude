package doctor

import (
	"context"
	"strings"
	"testing"

	"multiclaude/internal/registry"
)

func dupAccts() []registry.Account {
	return []registry.Account{{Name: "main"}, {Name: "admin", Dir: "/d/admin"}, {Name: "w", Dir: "/d/w"}}
}

func withIdentity(d Deps, ids map[string][2]string) Deps {
	d.Identity = func(_ context.Context, a registry.Account) (string, string) {
		return ids[a.Name][0], ids[a.Name][1]
	}
	return d
}

func goodScenario() scenario { return scenario{keychainOK: true, t3OK: true} }

func TestDoctorReportsDuplicateLoginAsInfoNotProblem(t *testing.T) {
	d, _ := build(goodScenario(), dupAccts())
	d = withIdentity(d, map[string][2]string{"main": {"u1", "a@x.io"}, "admin": {"u1", "a@x.io"}, "w": {"u2", "w@x.io"}})
	cs := Run(context.Background(), d, false)
	code, text := Summary(cs)
	if code != 0 {
		t.Fatalf("info must not affect exit code:\n%s", text)
	}
	if !strings.Contains(text, "i [admin] same login as main — shown as one row\n") {
		t.Fatalf("missing info line:\n%s", text)
	}
	if strings.Contains(text, "[w] same login") || strings.Contains(text, "u1") {
		t.Fatalf("unexpected:\n%s", text)
	}
	if !strings.Contains(text, "doctor: all good") {
		t.Fatalf("should still be all good:\n%s", text)
	}
}

func TestDoctorNoDuplicateWhenLoginsDiffer(t *testing.T) {
	d, _ := build(goodScenario(), dupAccts())
	d = withIdentity(d, map[string][2]string{"main": {"u1", "a@x.io"}, "admin": {"u3", "a@x.io"}, "w": {"", "w@x.io"}})
	if _, text := Summary(Run(context.Background(), d, false)); strings.Contains(text, "same login") {
		t.Fatalf("%s", text)
	}
}

func TestDoctorDuplicateWithProblemsStillFails(t *testing.T) {
	s := goodScenario()
	s.drift = true
	d, _ := build(s, dupAccts())
	d = withIdentity(d, map[string][2]string{"main": {"u1", ""}, "admin": {"u1", ""}, "w": {"u2", ""}})
	code, text := Summary(Run(context.Background(), d, false))
	if code != 1 || !strings.Contains(text, "same login as main") {
		t.Fatalf("%d\n%s", code, text)
	}
}
