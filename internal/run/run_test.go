package run

import (
	"context"
	"testing"
)

func TestOSRunSuccess(t *testing.T) {
	res, err := OS.Run(context.Background(), "/bin/echo", []string{"hi"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 || res.Stdout != "hi\n" {
		t.Fatalf("got %+v", res)
	}
}

func TestOSRunNonZeroIsNotError(t *testing.T) {
	res, err := OS.Run(context.Background(), "/usr/bin/false", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 1 {
		t.Fatalf("got %+v", res)
	}
}

func TestOSRunKilledBySignalMapsTo128PlusSig(t *testing.T) {
	res, err := OS.Run(context.Background(), "/bin/sh", []string{"-c", "kill -9 $$"}, nil, false)
	if err != nil || res.Code != 137 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestOSRunStartFailureIsError(t *testing.T) {
	if _, err := OS.Run(context.Background(), "/nonexistent/bin", nil, nil, false); err == nil {
		t.Fatal("expected error")
	}
}

func TestOSRunUsesProvidedEnv(t *testing.T) {
	res, err := OS.Run(context.Background(), "/usr/bin/env", nil, []string{"FOO=bar"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "FOO=bar\n" {
		t.Fatalf("got %q", res.Stdout)
	}
}

func TestFakeRecordsAndResponds(t *testing.T) {
	f := &Fake{Respond: func(c Call) (Result, error) { return Result{Code: 3, Stdout: c.Name}, nil }}
	res, err := f.Run(context.Background(), "x", []string{"a"}, []string{"E=1"}, true)
	if err != nil || res.Code != 3 || res.Stdout != "x" {
		t.Fatalf("got %+v %v", res, err)
	}
	if len(f.Calls) != 1 || f.Calls[0].Args[0] != "a" || !f.Calls[0].Inherit || f.Calls[0].Env[0] != "E=1" {
		t.Fatalf("calls %+v", f.Calls)
	}
}

func TestFakeNilRespond(t *testing.T) {
	f := &Fake{}
	res, err := f.Run(context.Background(), "x", nil, nil, false)
	if err != nil || res.Code != 0 {
		t.Fatalf("got %+v %v", res, err)
	}
}
