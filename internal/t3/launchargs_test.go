package t3

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const skip = "--dangerously-skip-permissions"

func launchArgsOf(t *testing.T, out []byte, id string) (string, bool) {
	t.Helper()
	var inst struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(instances(t, out)[id], &inst); err != nil {
		t.Fatal(err)
	}
	raw, ok := inst.Config["launchArgs"]
	var s string
	if ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s, ok
}

func desired(v string) LaunchArgs {
	return LaunchArgs{Desired: map[string]string{"main": v, "work": v}}
}

func TestMergeLaunchSetsLaunchArgsOnNewInstances(t *testing.T) {
	out, _, _, res, err := MergeLaunch([]byte(realFile), accts, shim, desired(skip))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"claude-main", "claude-work"} {
		if got, ok := launchArgsOf(t, out, id); !ok || got != skip {
			t.Errorf("%s: got %q ok=%v", id, got, ok)
		}
	}
	if want := map[string]string{"claude-main": skip, "claude-work": skip}; !reflect.DeepEqual(res.Written, want) {
		t.Fatalf("written=%v", res.Written)
	}
}

func TestMergeLaunchQuotedValueRoundTripsThroughJSON(t *testing.T) {
	val := `--append-system-prompt 'it'\''s "x"'`
	out, _, _, _, err := MergeLaunch([]byte(realFile), accts, shim, desired(val))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := launchArgsOf(t, out, "claude-work"); got != val {
		t.Fatalf("got %q", got)
	}
}

func TestMergeLaunchUpdatesValueItWroteBefore(t *testing.T) {
	first, _, _, res, _ := MergeLaunch([]byte(realFile), accts, shim, desired("--a"))

	out, _, updated, res2, err := MergeLaunch(first, accts, shim, LaunchArgs{Desired: desired("--b").Desired, Written: res.Written})

	if err != nil {
		t.Fatal(err)
	}
	if got, _ := launchArgsOf(t, out, "claude-work"); got != "--b" {
		t.Fatalf("got %q", got)
	}
	if len(updated) != 2 || res2.Written["claude-work"] != "--b" || len(res2.Warnings) != 0 {
		t.Fatalf("updated=%v res=%+v", updated, res2)
	}
}

func TestMergeLaunchPreservesUserEditedValueAndWarns(t *testing.T) {
	first, _, _, res, _ := MergeLaunch([]byte(realFile), accts, shim, desired("--a"))
	edited := strings.Replace(string(first), `"launchArgs": "--a"`, `"launchArgs": "--mine"`, 1)
	if edited == string(first) {
		t.Fatalf("setup: no launchArgs in\n%s", first)
	}

	out, _, _, res2, err := MergeLaunch([]byte(edited), accts, shim, LaunchArgs{Desired: desired("--b").Desired, Written: res.Written})

	if err != nil {
		t.Fatal(err)
	}
	got, _ := launchArgsOf(t, out, "claude-main")
	if got != "--mine" {
		t.Fatalf("user value clobbered: %q", got)
	}
	warn := "t3: claude-main launchArgs edited in T3; leaving it (ccm wants: --b)"
	if !slicesContain(res2.Warnings, warn) {
		t.Fatalf("warnings=%q", res2.Warnings)
	}
	if res2.Written["claude-main"] != "--a" {
		t.Fatalf("tracking changed: %v", res2.Written)
	}
}

func TestMergeLaunchAdoptsUserValueWhenNothingWrittenYetButNonEmpty(t *testing.T) {
	settings := `{"providerInstances":{"claude-work":{"driver":"claudeAgent","enabled":true,"config":{"binaryPath":"` + shim + `","homePath":"` + workDir + `","launchArgs":"--user"}}}}`

	out, _, _, res, err := MergeLaunch([]byte(settings), accts, shim, desired(skip))

	if err != nil {
		t.Fatal(err)
	}
	if got, _ := launchArgsOf(t, out, "claude-work"); got != "--user" {
		t.Fatalf("got %q", got)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "claude-work") {
		t.Fatalf("warnings=%q", res.Warnings)
	}
}

func TestMergeLaunchEmptyWantLeavesAbsentAndUnwritten(t *testing.T) {
	out, _, _, res, err := MergeLaunch([]byte(realFile), accts, shim, LaunchArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := launchArgsOf(t, out, "claude-work"); ok || len(res.Written) != 0 {
		t.Fatalf("ok=%v written=%v", ok, res.Written)
	}
}

func TestMergeLaunchEmptyWantRemovesValueCcmWrote(t *testing.T) {
	first, _, _, res, _ := MergeLaunch([]byte(realFile), accts, shim, desired("--a"))

	out, _, updated, res2, err := MergeLaunch(first, accts, shim, LaunchArgs{Written: res.Written})

	if err != nil {
		t.Fatal(err)
	}
	if _, ok := launchArgsOf(t, out, "claude-work"); ok {
		t.Fatal("launchArgs should be removed")
	}
	if len(updated) != 2 || len(res2.Written) != 0 {
		t.Fatalf("updated=%v written=%v", updated, res2.Written)
	}
}

func TestMergeLaunchEmptyWantKeepsUserEditedValue(t *testing.T) {
	first, _, _, res, _ := MergeLaunch([]byte(realFile), accts, shim, desired("--a"))
	edited := strings.Replace(string(first), `"launchArgs": "--a"`, `"launchArgs": "--mine"`, 1)

	out, _, _, _, _ := MergeLaunch([]byte(edited), accts, shim, LaunchArgs{Written: res.Written})

	if got, _ := launchArgsOf(t, out, "claude-main"); got != "--mine" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeLaunchIdempotent(t *testing.T) {
	first, _, _, res, _ := MergeLaunch([]byte(realFile), accts, shim, desired(skip))

	out, added, updated, res2, err := MergeLaunch(first, accts, shim, LaunchArgs{Desired: desired(skip).Desired, Written: res.Written})

	if err != nil || len(added)+len(updated) != 0 || string(out) != string(first) || !reflect.DeepEqual(res2.Written, res.Written) {
		t.Fatalf("added=%v updated=%v err=%v", added, updated, err)
	}
}

func TestMergeLaunchRecordsValueAlreadyEqual(t *testing.T) {
	settings := `{"providerInstances":{"claude-work":{"driver":"claudeAgent","enabled":true,"config":{"binaryPath":"` + shim + `","homePath":"` + workDir + `","launchArgs":"` + skip + `"}}}}`

	_, _, _, res, err := MergeLaunch([]byte(settings), accts[1:], shim, desired(skip))

	if err != nil || res.Written["claude-work"] != skip || len(res.Warnings) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func slicesContain(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
