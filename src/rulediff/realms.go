package rulediff

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/internal/realmfs"
	"github.com/SteerSpec/strspc-manager/src/internal/realmkind"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const manifestName = "realm.json"

// CompareRealms validates what a change does to the Realms of two whole trees,
// baseDir the old version and headDir the new, which no comparison of one
// entity file can see, as an entity file needn't change for any of it:
//
//   - RD028: the kind of a Realm changed, by its field or by the manifests
//     around a file that was already there. A Standard Realm's rules aren't
//     evaluated (STDRLM-011), so this is how evaluation would be switched off.
//   - RD029, and RD011, RD013, RD014 and RD015: Realm Notes (NTE-035, NTE-008,
//     NTE-009).
//   - RD030: a rule of a Standard Realm came to supersede a rule that was live
//     neither before the change nor after it (STDRLM-017).
//
// It doesn't compare entity files otherwise: Compare and CompareNew do. Both
// directories must exist, so a wrong base fails instead of comparing with
// nothing; pass an empty one for a new tree. baseDir needs every file of the
// tree, manifests included. Paths are relative to headDir.
func CompareRealms(baseDir, headDir string, opts ...Option) *result.Result {
	if baseDir == "" {
		res := &result.Result{}
		addReadError(res, baseDir, errors.New("no base directory"))
		return res
	}
	return compareRealms(baseDir, headDir, opts...)
}

// compareRealms is CompareRealms, with an empty baseDir for no base.
func compareRealms(baseDir, headDir string, opts ...Option) *result.Result {
	c := &realmComparison{
		res:       &result.Result{},
		baseKinds: map[string]*realmkind.Finder{},
		headKinds: map[string]*realmkind.Finder{},
	}
	c.run(baseDir, headDir)
	if New(opts...).cfg.Strict {
		promoteWarnings(c.res)
	}
	return c.res
}

// realmComparison holds one CompareRealms.
type realmComparison struct {
	res        *result.Result
	base, head string // resolved roots; base is empty when there is none.
	// A Finder for each root a source could have, by slash path below the tree.
	baseKinds, headKinds map[string]*realmkind.Finder
}

func (c *realmComparison) run(baseDir, headDir string) {
	var err error
	if c.head, err = resolveDir(headDir); err != nil {
		addReadError(c.res, headDir, err)
		return
	}
	if baseDir != "" {
		if c.base, err = resolveDir(baseDir); err != nil {
			addReadError(c.res, baseDir, err)
			return
		}
	}

	manifests := map[string]bool{}
	if c.base != "" {
		baseManifests, _ := c.scan(c.base)
		for _, rel := range baseManifests {
			manifests[rel] = true
		}
	}
	headManifests, headFiles := c.scan(c.head)
	for _, rel := range headManifests {
		manifests[rel] = true
	}

	for _, rel := range slices.Sorted(maps.Keys(manifests)) {
		c.compareManifest(rel)
	}
	for _, rel := range headFiles {
		c.compareFile(rel)
	}
}

// resolveDir resolves a tree's root, links in the path naming it included, to
// the real directory it names (realmfs.Root). An empty dir is refused: it
// would resolve to the working directory.
func resolveDir(dir string) (string, error) {
	return realmfs.Root(dir)
}

// scan lists the manifests and the other JSON files below root, outside
// _schema directories. In head, an entry that is a link or a special file is
// RD000, as is a name that is realm.json but for case, or two names in one
// directory that differ only by case: a reader on another system would see
// other files. In base, such entries are skipped, never followed, so that the
// change removing them passes.
func (c *realmComparison) scan(root string) (manifests, files []string) {
	head := root == c.head
	names := map[string][]string{} // directory → names seen, for case collisions
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			addReadError(c.res, path, err)
			return nil
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if head {
			dir, name := filepath.Dir(rel), d.Name()
			for _, seen := range names[dir] {
				if strings.EqualFold(seen, name) {
					addReadError(c.res, filepath.ToSlash(rel), fmt.Errorf("%q and %q differ only by case", seen, name))
				}
			}
			names[dir] = append(names[dir], name)
			if err := realmfs.CheckEntry(d); err != nil {
				addReadError(c.res, filepath.ToSlash(rel), err)
				return nil
			}
			if name != manifestName && strings.EqualFold(name, manifestName) {
				addReadError(c.res, filepath.ToSlash(rel), fmt.Errorf("a manifest must be named %s exactly", manifestName))
				return nil
			}
		} else if realmfs.CheckEntry(d) != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "_schema" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		if d.Name() == manifestName {
			manifests = append(manifests, filepath.ToSlash(rel))
		} else {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		addReadError(c.res, root, err)
	}
	return manifests, files
}

// readIn returns the file at rel below root, or nil when there is none. In
// head, a link there is an error; in base, it is never followed and reads as
// no file, so that the change replacing it passes.
func (c *realmComparison) readIn(root, rel string) ([]byte, error) {
	if root == "" {
		return nil, nil
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := realmfs.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.Is(err, realmfs.ErrNotRegular) && root != c.head:
		return nil, nil
	}
	return data, err
}

// compareManifest runs the checks on one realm.json.
func (c *realmComparison) compareManifest(rel string) {
	parse := func(root string) (*entity.RealmFile, []byte, bool) {
		data, err := c.readIn(root, rel)
		if err != nil {
			addReadError(c.res, rel, err)
			return nil, nil, false
		}
		if data == nil {
			return nil, nil, true
		}
		rf, err := entity.ParseRealm(data)
		if err != nil {
			addReadError(c.res, rel, err)
			return nil, data, false
		}
		return rf, data, true
	}
	base, baseData, baseOK := parse(c.base)
	head, headData, headOK := parse(c.head)
	if !baseOK || !headOK || bytes.Equal(baseData, headData) {
		return
	}

	var baseNotes, headNotes []entity.RealmNote
	if base != nil {
		baseNotes = base.Notes
	}
	if head != nil {
		headNotes = head.Notes
	}
	if base != nil && head != nil && base.Realm.Kind != head.Realm.Kind {
		c.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD028",
			Severity: result.Error,
			Message:  fmt.Sprintf("realm.kind changed from %s to %s: a Realm keeps its kind, which decides whether its rules are evaluated (STDRLM-011); create a new Realm instead", kindName(base.Realm.Kind), kindName(head.Realm.Kind)),
			Path:     rel,
		})
	}
	checkRealmNotes(baseNotes, headNotes, c.res, rel)
}

// compareFile runs the checks that need the kind of the Realm holding a file.
// A source may be rooted at any directory above a file, and a root changes
// which manifests count, so each is tried.
func (c *realmComparison) compareFile(rel string) {
	existed := false
	if c.base != "" {
		info, err := os.Lstat(filepath.Join(c.base, filepath.FromSlash(rel)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			addReadError(c.res, rel, err)
			return
		}
		existed = err == nil && !info.IsDir()
	}

	changed, standard := false, false
	root, rest := ".", rel
	for {
		was, now := "", kindIn(c.headKinds, c.head, root, rel)
		if existed {
			was = kindIn(c.baseKinds, c.base, root, rel)
			if was != now && !changed {
				changed = true
				c.res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RD028",
					Severity: result.Error,
					Message:  fmt.Sprintf("file moved from a %s Realm to a %s Realm for a source rooted at %q: the manifests around rules that already exist may not change their kind (STDRLM-011); add a new Realm with new files instead", kindName(was), kindName(now), root),
					Path:     rel,
				})
			}
		}
		standard = standard || now == entity.KindStandard
		dir, below, ok := strings.Cut(rest, "/")
		if !ok {
			break
		}
		if root == "." {
			root = dir
		} else {
			root += "/" + dir
		}
		rest = below
	}
	if standard {
		c.checkSupersession(rel, existed)
	}
}

// kindIn returns the kind of the file at rel for a source rooted at root, both
// below tree.
func kindIn(finders map[string]*realmkind.Finder, tree, root, rel string) string {
	f, ok := finders[root]
	if !ok {
		f = realmkind.New(filepath.Join(tree, filepath.FromSlash(root)))
		finders[root] = f
	}
	return f.KindOf(filepath.Join(tree, filepath.FromSlash(rel)))
}

func kindName(kind string) string {
	if kind == "" {
		return "code"
	}
	return fmt.Sprintf("%q", kind)
}

// checkSupersession runs RD030 on a file of a Standard Realm. A file that
// doesn't parse is left to Compare's RD000.
func (c *realmComparison) checkSupersession(rel string, existed bool) {
	headData, err := c.readIn(c.head, rel)
	if err != nil {
		addReadError(c.res, rel, err)
		return
	}
	var baseData []byte
	if existed {
		if baseData, err = c.readIn(c.base, rel); err != nil {
			addReadError(c.res, rel, err)
			return
		}
		if bytes.Equal(baseData, headData) {
			return
		}
	}
	head, err := decodeEntity(headData)
	if err != nil {
		return
	}
	var base *entity.File
	if existed {
		if parsed, err := decodeEntity(baseData); err == nil {
			base = parsed
		}
	}
	checkSupersedesLive(base, head, c.res, rel)
}

// checkSupersedesLive runs RD030 on one entity and its sub-entities: when a
// rule's `supersedes` is set or changed, or the rule leaves Draft, its target
// must have been live before the change or be live after it (STDRLM-017). The
// first covers a fix that terminates its target in the change that publishes
// it (STDRLM-013), the second a target published in this change.
func checkSupersedesLive(base, head *entity.File, res *result.Result, path string) {
	before := map[string]entity.Rule{}
	oldSubs := map[string]*entity.File{}
	if base != nil {
		before = buildRuleMap(base.Rules)
		for i := range base.SubEntities {
			oldSubs[base.SubEntities[i].Entity.ID] = &base.SubEntities[i]
		}
	}
	after := buildRuleMap(head.Rules)

	for _, h := range head.Rules {
		if h.Supersedes == nil || h.State == entity.StateAbandoned {
			continue
		}
		if b, ok := before[h.ID]; ok && sameRef(b.Supersedes, h.Supersedes) &&
			(b.State != entity.StateDraft || h.State == entity.StateDraft) {
			continue
		}
		was, inBase := before[*h.Supersedes]
		now, inHead := after[*h.Supersedes]
		if !inBase && !inHead {
			continue // No such rule: RL012 reports it.
		}
		if (inBase && entity.IsLiveStandardRule(was)) || (inHead && entity.IsLiveStandardRule(now)) {
			continue
		}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD030",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule %s supersedes %s, which was live neither before this change nor after it: supersede the live rule (STDRLM-017)", h.ID, *h.Supersedes),
			Path:     path + "/" + h.ID,
		})
	}
	for i := range head.SubEntities {
		sub := &head.SubEntities[i]
		checkSupersedesLive(oldSubs[sub.Entity.ID], sub, res, path+" > "+sub.Entity.ID)
	}
}

// checkRealmNotes runs the note checks on the Realm Notes of one manifest:
// none is removed (RD014), each change moves the revision by one (RD013), a new
// one starts at 0 with an author (RD015, RD011), and the only change of type or
// ref is an exclusion's withdrawal to reference, which keeps ref and content
// (RD029, NTE-035).
func checkRealmNotes(baseNotes, headNotes []entity.RealmNote, res *result.Result, path string) {
	before := make(map[string]entity.RealmNote, len(baseNotes))
	for _, n := range baseNotes {
		before[n.ID] = n
	}
	kept := make(map[string]bool, len(headNotes))
	for _, h := range headNotes {
		kept[h.ID] = true
		notePath := path + "/notes/" + h.ID
		asNote := entity.Note{Type: h.Type, Content: h.Content, AddedBy: h.AddedBy, Revision: h.Revision}
		var msg string
		if b, ok := before[h.ID]; ok {
			checkNoteRevision(entity.Note{Type: b.Type, Content: b.Content, Revision: b.Revision}, asNote, res, notePath)
			withdrawn := b.Type == entity.NoteTypeExclusion && h.Type == entity.NoteTypeReference
			switch {
			case h.Ref != b.Ref:
				msg = fmt.Sprintf("ref changed from %q to %q: the ref of a Realm Note never changes", b.Ref, h.Ref)
			case h.Type != b.Type && !withdrawn:
				msg = fmt.Sprintf("type changed from %q to %q: the only change of type is withdrawing an exclusion to reference", b.Type, h.Type)
			case withdrawn && h.Content != b.Content:
				msg = "content changed: withdrawing an exclusion keeps its content"
			}
		} else {
			checkNewNote(asNote, res, notePath)
			if h.Type == entity.NoteTypeReference && h.Ref != "" {
				msg = "a new reference has a ref: only a withdrawn exclusion is a reference with a ref"
			}
		}
		if msg != "" {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD029",
				Severity: result.Error,
				Message:  fmt.Sprintf("Realm Note %s: %s (NTE-035)", h.ID, msg),
				Path:     notePath,
			})
		}
	}
	for _, b := range baseNotes {
		if !kept[b.ID] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD014",
				Severity: result.Error,
				Message:  fmt.Sprintf("Realm Note %q (type %q) removed: withdraw or revise it in place instead of deleting (NTE-009)", b.ID, b.Type),
				Path:     path + "/notes/" + b.ID,
			})
		}
	}
}

func addReadError(res *result.Result, path string, err error) {
	res.Add(result.Diagnostic{
		Module:   module,
		Code:     "RD000",
		Severity: result.Error,
		Message:  "failed to read " + path + " to compare Realms: " + err.Error(),
		Path:     path,
	})
}
