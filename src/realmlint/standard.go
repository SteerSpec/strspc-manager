package realmlint

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// The Realm Note types other than exclusion and reference (NTE-031).
const (
	noteTypeRationale     = "rationale"
	noteTypeClarification = "clarification"
)

// realmNoteIDRe matches a Realm Note identifier, capturing its number
// (NTE-030).
var realmNoteIDRe = regexp.MustCompile(`^REALM/(\d{3})$`)

// checkManifestNotesAndHash runs the manifest checks core 1.21.0 adds that the
// schema can't express: RM021 (STDRLM-002, STDRLM-003, STDRLM-015), RM022
// (NTE-030), RM023 (NTE-034), RM024 (NTE-035) and RM025 (RLMMNFST-013). data
// is the manifest as read, which RM025 hashes. The schema (RM002) already
// checks the kind, the shape of attribution and profiles, the form of a note
// id, the note types and the form of a ref and hash.
func checkManifestNotesAndHash(rf *entity.RealmFile, data []byte, realmPath string, res *result.Result) {
	checkStandardRealmFields(&rf.Realm, realmPath, res)
	checkRealmNoteIDs(rf.Notes, realmPath, res)
	checkRealmNoteRefs(rf, realmPath, res)
	checkManifestHash(rf, data, realmPath, res)
}

// checkStandardRealmFields runs RM021: a Standard Realm declares an edition
// (STDRLM-002), a license (STDRLM-003) and an attribution (STDRLM-015). The
// kind is read from this manifest only, so a sub-realm without one is a code
// Realm (RLMMNFST-009).
func checkStandardRealmFields(meta *entity.RealmMeta, realmPath string, res *result.Result) {
	if meta.Kind != entity.KindStandard {
		return
	}
	missing := func(field, rule string) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM021",
			Severity: result.Error,
			Message:  fmt.Sprintf("Standard Realm %s declares no %s (%s)", meta.ID, field, rule),
			Path:     realmPath,
		})
	}
	if meta.Edition == "" {
		missing("edition", "STDRLM-002")
	}
	if meta.License == "" {
		missing("license", "STDRLM-003")
	}
	if meta.Attribution == nil {
		missing("attribution", "STDRLM-015")
	}
}

// checkRealmNoteIDs runs RM022: Realm Note identifiers are REALM/NNN,
// sequential from 001 (NTE-030), with no number repeated or skipped. As with
// rule notes (RL015), the order of the array doesn't matter. An identifier of
// another form is left to the schema (RM002).
func checkRealmNoteIDs(notes []entity.RealmNote, realmPath string, res *result.Result) {
	seen := make(map[int]bool, len(notes))
	highest := 0
	for _, n := range notes {
		m := realmNoteIDRe.FindStringSubmatch(n.ID)
		if m == nil {
			continue // RM002.
		}
		num, _ := strconv.Atoi(m[1]) // Three digits always parse.
		if seen[num] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RM022",
				Severity: result.Error,
				Message:  fmt.Sprintf("Realm Note %s is declared more than once (NTE-030)", n.ID),
				Path:     realmPath,
			})
		}
		seen[num] = true
		highest = max(highest, num)
	}
	for num := 1; num < highest; num++ {
		if !seen[num] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RM022",
				Severity: result.Error,
				Message:  fmt.Sprintf("Realm Note REALM/%03d is missing: Realm Notes are numbered from REALM/001 with no gap (NTE-030)", num),
				Path:     realmPath,
			})
		}
	}
	if seen[0] {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM022",
			Severity: result.Error,
			Message:  "Realm Note REALM/000 is not allowed: Realm Notes are numbered from REALM/001 (NTE-030)",
			Path:     realmPath,
		})
	}
}

// checkRealmNoteRefs runs RM023 and RM024 on the ref of each Realm Note. An
// exclusion has a ref whose <id> names a Standard Reference with a realm in
// this manifest (NTE-034): RM023. Only an exclusion, or the reference an
// exclusion becomes when withdrawn (NTE-035), has a ref, so a rationale or
// clarification with one is RM024. Whether a reference with a ref was an
// exclusion before needs the history, so that is left to rule-diff. A type
// outside NTE-031 is left to the schema (RM002).
func checkRealmNoteRefs(rf *entity.RealmFile, realmPath string, res *result.Result) {
	for _, n := range rf.Notes {
		switch n.Type {
		case entity.NoteTypeExclusion:
			if msg := exclusionRefProblem(n.Ref, rf.Realm.Standards); msg != "" {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RM023",
					Severity: result.Error,
					Message:  fmt.Sprintf("exclusion %s: %s (NTE-034)", n.ID, msg),
					Path:     realmPath,
				})
			}
		case noteTypeRationale, noteTypeClarification:
			if n.Ref != "" {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RM024",
					Severity: result.Error,
					Message:  fmt.Sprintf("Realm Note %s of type %s has a ref; only an exclusion, or a reference withdrawn from one, has a ref (NTE-035)", n.ID, n.Type),
					Path:     realmPath,
				})
			}
		}
	}
}

// exclusionRefProblem describes what is wrong with the ref of an exclusion, or
// returns "" when its <id> names a Standard Reference with a realm in standards.
func exclusionRefProblem(ref string, standards []entity.StandardRef) string {
	if ref == "" {
		return "has no ref"
	}
	id, _, ok := strings.Cut(ref, ":")
	if !ok {
		return fmt.Sprintf("ref %q is not <id>:<clause>", ref)
	}
	std := entity.FindStandard(standards, id)
	switch {
	case std == nil:
		return fmt.Sprintf("ref %q names standard %q, which this manifest doesn't declare", ref, id)
	case std.Realm == "":
		return fmt.Sprintf("ref %q names standard %q, which links to no Standard Realm", ref, id)
	}
	return ""
}

// checkManifestHash runs RM025: a manifest that declares a hash declares the
// one ComputeRealmHash gives for it (RLMMNFST-013). A manifest without a hash
// isn't reported: until the CLI recomputes the hash on every write
// (RLMMNFST-014), the schema keeps it optional.
func checkManifestHash(rf *entity.RealmFile, data []byte, realmPath string, res *result.Result) {
	if rf.Hash == "" {
		return
	}
	got, err := entity.ComputeRealmHash(data)
	if err != nil {
		return // The manifest parsed, so this can't fail; RM001 reports what does.
	}
	if got != rf.Hash {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM025",
			Severity: result.Error,
			Message:  fmt.Sprintf("manifest hash %s does not match its content, which hashes to %s (RLMMNFST-013)", rf.Hash, got),
			Path:     realmPath,
		})
	}
}
