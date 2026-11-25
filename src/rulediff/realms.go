package rulediff

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/internal/realmfs"
	"github.com/SteerSpec/strspc-manager/src/internal/realmkind"
	"github.com/SteerSpec/strspc-manager/src/remote"
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
	cfg := New(opts...).cfg
	c := &realmComparison{
		res:          &result.Result{},
		baseKinds:    map[string]*realmkind.Finder{},
		headKinds:    map[string]*realmkind.Finder{},
		contributors: cfg.Contributors,
		baseIDs:      map[string]map[string]string{},
		headIDs:      map[string]map[string]string{},
		baseDeps:     map[string][]depSource{},
		headDeps:     map[string][]depSource{},
	}
	c.run(baseDir, headDir)
	if cfg.Strict {
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
	// contributors is the Realm's list (WithContributors), for RD023 on new
	// Realm Notes. A sub-realm uses its parent's (CTRB-006).
	contributors []entity.Contributor
	// kinds records, by realm ID, the kinds and manifests seen in each tree.
	baseIDs, headIDs map[string]map[string]string
	// deps records, by dependency realm ID, the sources each tree declares.
	baseDeps, headDeps map[string][]depSource
}

// depSource is a dependency source declared by the manifest at rel.
type depSource struct{ source, rel string }

func (c *realmComparison) run(baseDir, headDir string) {
	var err error
	if c.head, err = resolveDir(headDir); err != nil {
		addReadError(c.res, ".", fmt.Errorf("%s: %w", headDir, err)) // Paths are relative to headDir.
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
	c.compareKindsByID()
	c.compareDependencySources()
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
	names := map[string]string{} // directory and folded name → name seen, for case collisions
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if err != nil {
			addReadError(c.res, filepath.ToSlash(rel), err)
			return nil
		}
		if path == root {
			return nil
		}
		if head {
			dir, name := filepath.Dir(rel), d.Name()
			key := dir + "/" + realmfs.FoldKey(name)
			if seen, ok := names[key]; ok {
				addReadError(c.res, filepath.ToSlash(rel), fmt.Errorf("%q and %q differ only by case", seen, name))
			} else {
				names[key] = name
			}
			if skip, err := realmfs.CheckWalked(path, d); err != nil {
				addReadError(c.res, filepath.ToSlash(rel), err)
				return nil
			} else if skip {
				return nil
			}
			if name != manifestName && strings.EqualFold(name, manifestName) {
				addReadError(c.res, filepath.ToSlash(rel), fmt.Errorf("a manifest must be named %s exactly", manifestName))
				return nil
			}
		} else if realmfs.CheckEntry(d) != nil {
			// Read only when it leads to a file inside the base tree.
			if !strings.HasSuffix(d.Name(), ".json") || !readable(root, path) {
				return nil
			}
		}
		if d.IsDir() {
			if d.Name() == "_schema" {
				if head {
					c.checkNoLinks(path, root)
				}
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
		addReadError(c.res, ".", err)
	}
	return manifests, files
}

// checkNoLinks reports RD000 for each link or special file below dir, a
// directory of head whose files aren't compared, such as _schema.
func (c *realmComparison) checkNoLinks(dir, root string) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil
		}
		if cerr := realmfs.CheckEntry(d); cerr != nil {
			rel, _ := filepath.Rel(root, path)
			addReadError(c.res, filepath.ToSlash(rel), cerr)
		}
		return nil
	})
}

// readIn returns the file at rel below root, or nil when there is none. In
// head, a link there is an error; in base, a link is followed only to a file
// inside the base tree, and otherwise reads as no file, so that the change
// replacing it passes.
func (c *realmComparison) readIn(root, rel string) ([]byte, error) {
	if root == "" {
		return nil, nil
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	read := realmfs.ReadFile
	if root != c.head {
		read = func(p string) ([]byte, error) { return realmfs.ReadInTree(root, p) }
	}
	data, err := read(path)
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
			if root != c.head {
				// A broken base holds nothing to keep: the change repairing or
				// deleting it is judged by its head alone.
				return nil, data, true
			}
			addReadError(c.res, rel, err)
			return nil, data, false
		}
		return rf, data, true
	}
	base, baseData, baseOK := parse(c.base)
	head, headData, headOK := parse(c.head)
	record(c.baseIDs, base, rel)
	record(c.headIDs, head, rel)
	recordDeps(c.baseDeps, base, rel)
	recordDeps(c.headDeps, head, rel)
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
	if base != nil && head != nil && kindChanged(base.Realm.Kind, head.Realm.Kind) {
		c.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD028",
			Severity: result.Error,
			Message:  fmt.Sprintf("realm.kind changed from %s to %s: a Realm keeps its kind, which decides whether its rules are evaluated (STDRLM-011); create a new Realm instead", kindName(base.Realm.Kind), kindName(head.Realm.Kind)),
			Path:     rel,
		})
	}
	checkRealmNotes(baseNotes, headNotes, c.contributors, c.res, rel)
}

// recordDeps notes the dependency sources rf declares, at rel, in deps.
func recordDeps(deps map[string][]depSource, rf *entity.RealmFile, rel string) {
	if rf == nil {
		return
	}
	for _, d := range rf.Dependencies {
		deps[d.RealmID] = append(deps[d.RealmID], depSource{source: d.Source, rel: rel})
	}
}

// kindChanged reports whether a manifest's kind changed from was to now in a
// way RD028 forbids: what it evaluates as changed, or a valid kind became
// another value. Repairing an invalid kind to the one it evaluates as passes.
func kindChanged(was, now string) bool {
	standard := func(k string) bool { return k == entity.KindStandard }
	if standard(was) != standard(now) {
		return true
	}
	return was != now && (was == "" || standard(was))
}

// record notes the kind the Realm rf declares evaluates as, at rel, in ids.
func record(ids map[string]map[string]string, rf *entity.RealmFile, rel string) {
	if rf == nil || rf.Realm.ID == "" {
		return
	}
	if ids[rf.Realm.ID] == nil {
		ids[rf.Realm.ID] = map[string]string{}
	}
	kind := "" // Evaluated as code (STDRLM-011): any kind but standard.
	if rf.Realm.Kind == entity.KindStandard {
		kind = entity.KindStandard
	}
	if _, ok := ids[rf.Realm.ID][kind]; !ok {
		ids[rf.Realm.ID][kind] = rel
	}
}

// compareKindsByID runs RD028 by realm ID: a Realm keeps its kind wherever its
// manifest is, so a copy with its ID and another kind, at another path, is a
// change of kind too.
func (c *realmComparison) compareKindsByID() {
	for _, id := range slices.Sorted(maps.Keys(c.headIDs)) {
		was, ok := c.baseIDs[id]
		if !ok {
			continue
		}
		for _, kind := range slices.Sorted(maps.Keys(c.headIDs[id])) {
			if _, same := was[kind]; same {
				continue
			}
			for _, old := range slices.Sorted(maps.Keys(was)) {
				if was[old] == c.headIDs[id][kind] {
					continue // The same manifest: its field change is RD028 already.
				}
				c.res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RD028",
					Severity: result.Error,
					Message:  fmt.Sprintf("Realm %s is %s at %s in base and %s at %s: a Realm keeps its kind wherever its manifest is (STDRLM-011); create a new Realm instead", id, kindName(old), was[old], kindName(kind), c.headIDs[id][kind]),
					Path:     c.headIDs[id][kind],
				})
			}
		}
	}
}

// compareDependencySources runs RD031: a dependency keeps its realm ID, so the
// source it is fetched from may change only its ref, wherever it is declared.
// A head source for a realm ID that base depends on must name a tree a base
// source for that ID names: a sub-realm overriding the dependency, or a
// manifest at a new path, can't fetch another tree under the same ID.
func (c *realmComparison) compareDependencySources() {
	for _, id := range slices.Sorted(maps.Keys(c.headDeps)) {
		was, ok := c.baseDeps[id]
		if !ok {
			continue
		}
		for _, h := range c.headDeps[id] {
			var same []string
			kept := false
			for _, b := range was {
				if sameSource(b, h) {
					same = append(same, b.source)
					kept = kept || (b.rel == h.rel && b.source == h.source)
				}
			}
			msg := fmt.Sprintf("source %q names another tree than base declares (%q); only the ref of a github source may change, so add a dependency with a new realm ID instead", h.source, was[0].source)
			switch {
			case kept:
				msg = "" // The manifest declared this source already.
			case len(same) > 0:
				msg = refChange(same, h.source)
			}
			if msg == "" {
				continue
			}
			c.res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD031",
				Severity: result.Error,
				Message:  fmt.Sprintf("dependency %s: %s", id, msg),
				Path:     h.rel,
			})
		}
	}
}

// releaseTag matches the ref a changed github source may name: a release tag.
var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// refChange explains why a dependency declaration may move to source now, from
// the base sources was that name the same tree, or returns "". A github ref
// must be a release tag vX.Y.Z, no lower than any release tag base declares
// for the tree: a commit or a branch may come from a fork of the repository,
// which GitHub serves through it, and an older release may hold weaker rules.
// A local source naming the same directory passes.
func refChange(was []string, now string) string {
	after, err := remote.ParseGitHub(now)
	if err != nil {
		return "" // Local sources naming the same directory.
	}
	if !releaseTag.MatchString(after.Ref) {
		return fmt.Sprintf("ref %q is not a release tag vX.Y.Z: a changed github source must pin a release", after.Ref)
	}
	for _, w := range was {
		if before, err := remote.ParseGitHub(w); err == nil && releaseTag.MatchString(before.Ref) && isNewerSemver(after.Ref, before.Ref) {
			return fmt.Sprintf("ref %s is lower than %s, which base declares: a dependency may not move back to an older release", after.Ref, before.Ref)
		}
	}
	return ""
}

// sameSource reports whether two dependency sources name the same tree, but
// for the ref of a github source. A local source is relative to the directory
// of the manifest declaring it.
func sameSource(a, b depSource) bool {
	ra, errA := remote.ParseGitHub(a.source)
	rb, errB := remote.ParseGitHub(b.source)
	switch {
	case errA == nil && errB == nil:
		return strings.EqualFold(ra.Owner, rb.Owner) && strings.EqualFold(ra.Repo, rb.Repo) && ra.Path == rb.Path
	case errA == nil || errB == nil || strings.Contains(a.source, "://") || strings.Contains(b.source, "://"):
		return false
	}
	local := func(d depSource) string {
		return path.Join(path.Dir(d.rel), filepath.ToSlash(d.source))
	}
	return local(a) == local(b)
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
func checkRealmNotes(baseNotes, headNotes []entity.RealmNote, contributors []entity.Contributor, res *result.Result, path string) {
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
			checkContributor("Realm Note", h.AddedBy, contributors, res, notePath)
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
