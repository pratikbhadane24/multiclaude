package cli

import (
	"testing"

	"github.com/pratikbhadane24/multiclaude/internal/consts"
)

func TestEveryCommandNameIsReserved(t *testing.T) {
	for name := range commands {
		if _, ok := consts.ReservedNames[name]; !ok {
			t.Errorf("command %q is not in consts.ReservedNames", name)
		}
	}
}
