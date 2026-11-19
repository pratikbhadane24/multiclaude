package doctor

import (
	"context"
	"strings"
	"testing"
)

func TestDoctorLoggedOutAccountsNeverMerge(t *testing.T) {
	d, _ := build(goodScenario(), dupAccts())
	d = withIdentity(d, map[string][2]string{"main": {"u1", "a@x.io"}, "admin": {"", ""}, "w": {"", ""}})
	if _, text := Summary(Run(context.Background(), d, false)); strings.Contains(text, "same login") {
		t.Fatalf("%s", text)
	}
}
