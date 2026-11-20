package autoswitch

import (
	"testing"
	"time"
)

func apiErr(errField, text string) string {
	return `{"type":"assistant","isApiErrorMessage":true,"isSidechain":false,"error":"` + errField +
		`","message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}`
}

func TestDetectLimitRateLimitWithReset(t *testing.T) {
	// Arrange
	line := apiErr("rate_limit", "You've hit your 5-hour limit · resets 1:20am (Asia/Calcutta)")

	// Act
	l, ok := DetectLimit([]byte(line))

	// Assert
	if !ok || l.Weekly || l.Reset != "1:20am (Asia/Calcutta)" {
		t.Fatalf("got %+v ok=%v", l, ok)
	}
}

func TestDetectLimitWeekly(t *testing.T) {
	l, ok := DetectLimit([]byte(apiErr("rate_limit", "You've hit your weekly limit · resets Nov 3 at 10:30am (Asia/Calcutta)")))
	if !ok || !l.Weekly || l.Reset != "Nov 3 at 10:30am (Asia/Calcutta)" {
		t.Fatalf("got %+v ok=%v", l, ok)
	}
}

func TestDetectLimitRateLimitFieldWithoutPhrase(t *testing.T) {
	l, ok := DetectLimit([]byte(apiErr("rate_limit", "API Error: Request rejected (429) · This request would exceed your account's rate limit")))
	if !ok || l.Reset != "" {
		t.Fatalf("got %+v ok=%v", l, ok)
	}
}

func TestDetectLimitIgnoresOtherErrors(t *testing.T) {
	for _, line := range []string{
		apiErr("server_error", "API Error: Connection error (ECONNRESET)"),
		apiErr("billing_error", "Credit balance is too low"),
		apiErr("authentication_failed", "Please run /login"),
		apiErr("", "API Error: 529 Overloaded. You've hit your limit"),
		`{"type":"assistant","message":{"content":[{"type":"text","text":"You've hit your 5-hour limit"}]}}`,
		`{"type":"user","isApiErrorMessage":true,"error":"rate_limit"}`,
		`{"type":"assistant","isApiErrorMessage":true,"isSidechain":true,"error":"rate_limit"}`,
		`not json "isApiErrorMessage"`,
		``,
	} {
		if l, ok := DetectLimit([]byte(line)); ok {
			t.Errorf("line %q detected as %+v", line, l)
		}
	}
}

func TestDetectLimitTextFallbackWhenErrorFieldMissing(t *testing.T) {
	for _, text := range []string{
		"You've hit your limit · resets 3pm",
		"Claude usage limit reached. Your limit will reset at 3pm",
		"5-hour limit reached ∙ resets 3pm",
		"Weekly limit reached",
	} {
		if _, ok := DetectLimit([]byte(apiErr("", text))); !ok {
			t.Errorf("text %q not detected", text)
		}
	}
}

func TestDetectLimitStringContent(t *testing.T) {
	line := `{"type":"assistant","isApiErrorMessage":true,"message":{"content":"You've hit your weekly limit"}}`
	if l, ok := DetectLimit([]byte(line)); !ok || !l.Weekly {
		t.Fatalf("got %+v ok=%v", l, ok)
	}
}

func TestParseResetTimeOnlyRollsToTomorrow(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Calcutta")
	if err != nil {
		t.Skip("no tzdata")
	}
	now := time.Date(2026, 11, 20, 2, 0, 0, 0, loc)

	got, ok := ParseReset("1:20am (Asia/Calcutta)", now)

	if !ok || !got.Equal(time.Date(2026, 11, 21, 1, 20, 0, 0, loc)) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestParseResetTodayAndHourOnly(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	got, ok := ParseReset("10:30am (UTC)", now)
	if !ok || !got.Equal(time.Date(2026, 11, 20, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	got, ok = ParseReset("3pm (UTC)", now)
	if !ok || !got.Equal(time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestParseResetWithDate(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	got, ok := ParseReset("Nov 23 at 10:30am (UTC)", now)
	if !ok || !got.Equal(time.Date(2026, 11, 23, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	got, ok = ParseReset("Jan 2 at 1am (UTC)", now)
	if !ok || !got.Equal(time.Date(2027, 1, 2, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestParseResetRejectsGarbage(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	for _, s := range []string{"", "soon", "25:99pm (UTC)", "Foo 40 at 3pm (UTC)"} {
		if got, ok := ParseReset(s, now); ok {
			t.Errorf("%q parsed as %v", s, got)
		}
	}
}
