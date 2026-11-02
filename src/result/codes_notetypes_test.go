package result_test

import (
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// TestRL010FixListsEveryNoteType fails when a note type is added to the closed
// enum but not to the RL010 fix text, which tells users the valid types.
func TestRL010FixListsEveryNoteType(t *testing.T) {
	info, ok := result.LookupCode("RL010")
	if !ok {
		t.Fatal("RL010 is not in the registry")
	}
	for _, nt := range entity.NoteTypes() {
		if !strings.Contains(info.Fix, "`"+nt+"`") {
			t.Errorf("RL010 fix text omits note type %q: %s", nt, info.Fix)
		}
	}
}
