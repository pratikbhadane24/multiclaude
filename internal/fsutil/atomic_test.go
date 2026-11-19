package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreatesDirModeAndLeavesNoTemp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "f.json")

	if err := WriteAtomic(path, []byte("one"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("two"), 0o640); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	entries, _ := os.ReadDir(filepath.Dir(path))
	if string(b) != "two" || st.Mode().Perm() != 0o640 || len(entries) != 1 {
		t.Fatalf("b=%q mode=%v entries=%d", b, st.Mode(), len(entries))
	}
}
