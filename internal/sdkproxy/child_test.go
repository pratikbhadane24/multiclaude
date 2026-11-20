package sdkproxy

import (
	"io"
	"os"
	"syscall"
	"testing"
)

func TestExecStarterPipesAndExitCode(t *testing.T) {
	c, err := ExecStarter(os.Stderr)("/bin/sh", []string{"sh", "-c", "cat; exit 3"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stdin().Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	_ = c.Stdin().Close()
	out, err := io.ReadAll(c.Stdout())
	if err != nil || string(out) != "hello\n" {
		t.Fatalf("out %q err %v", out, err)
	}
	if code := c.Wait(); code != 3 {
		t.Fatalf("exit %d", code)
	}
}

func TestExecStarterSignalExitCode(t *testing.T) {
	c, err := ExecStarter(os.Stderr)("/bin/sh", []string{"sh", "-c", "exec sleep 30"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := c.Wait(); code != 128+int(syscall.SIGTERM) {
		t.Fatalf("exit %d", code)
	}
}

func TestExecStarterMissingBinary(t *testing.T) {
	if _, err := ExecStarter(os.Stderr)("/nonexistent/claude", []string{"claude"}, nil); err == nil {
		t.Fatal("want error")
	}
}
