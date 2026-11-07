package coverage

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
)

// Figure is a coverage figure for a Standard Reference with a `realm`
// (STDREF-024, STDREF-025): Covered of Total, where Total is the clause list
// of the linked Standard Realm, narrowed to the reference's profile
// (STDREF-020), less the clauses excluded (STDREF-022). It holds counts, not a
// ratio, so a figure whose Total is 0 stays undefined (STDREF-028) and is never
// read as 0 % or 100 %.
type Figure struct {
	// Tree is the ID of the Realm tree measured.
	Tree string
	// Entity is the EUID of the entity measured, or empty for the Realm
	// figure, which counts a clause covered in any entity of the tree
	// (STDREF-025).
	Entity string
	// Standard, Version, Realm and Profile are the reference's.
	Standard, Version, Realm, Profile string
	// Covered is the number of clauses of the list, not excluded, covered.
	Covered int
	// Total is the number of clauses of the list less those excluded.
	Total int
	// Excluded is the number of clauses of the list excluded.
	Excluded int
	// Defined is false when Total is 0 (STDREF-028).
	Defined bool
}

// Links holds what Compute needs to measure entities against the Standard
// Realms their references link: for each Realm of the trees added, its
// manifest, whose `exclusion` Realm Notes exclude clauses (STDREF-022), and
// the resolved dependencies its references' `realm` resolves against. It
// fetches nothing: realm-resolve did.
type Links struct {
	realms []*linkRealm
	// loaded caches the Standard Realm read from each dependency directory,
	// nil when it isn't one or can't be read.
	loaded   map[string]*entity.StandardRealm
	warnings []string
}

// linkRealm is one Realm of a tree added to Links.
type linkRealm struct {
	dir  string
	rf   *entity.RealmFile
	tree *linkTree
	// deps holds the dependency lists the Realm's references resolve
	// against, nearest first: its own effective dependencies, which
	// realm-resolve builds with a sub-realm's own pin first (RLM-014), then
	// those of the rest of the tree (STDREF-017).
	deps [][]*realmresolve.ResolvedRealm
}

// linkTree is a Realm tree added to Links.
type linkTree struct {
	realms []*linkRealm
	// parent is the Realm that lists the tree's root as a sub-realm, when a
	// sub-realm was resolved on its own: its manifest's references reach the
	// root's entities (NTE-016), with its exclusions. Its dependencies were
	// not resolved, so a reference resolves its `realm` against the tree's.
	parent *linkRealm
}

// NewLinks returns an empty Links.
func NewLinks() *Links {
	return &Links{loaded: make(map[string]*entity.StandardRealm)}
}

// AddTree adds the Realm tree whose manifest rf is in dir, as res resolved it:
// the Realm and the sub-realms of res.SubRealms.
func (l *Links) AddTree(dir string, rf *entity.RealmFile, res *realmresolve.ResolveResult) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("coverage links: %w", err)
	}
	tree := &linkTree{}
	if parent := entity.ParentRealm(abs, rf.Realm.ID); parent != nil {
		tree.parent = &linkRealm{dir: filepath.Dir(abs), rf: parent, tree: tree}
	}
	root := &linkRealm{dir: abs, rf: rf, tree: tree, deps: [][]*realmresolve.ResolvedRealm{res.Dependencies}}
	tree.realms = append(tree.realms, root)
	for _, sub := range res.SubRealms {
		tree.realms = append(tree.realms, &linkRealm{dir: sub.Dir, rf: sub.Realm, tree: tree, deps: [][]*realmresolve.ResolvedRealm{sub.Dependencies}})
	}
	// After its own, each Realm resolves against the others of the tree.
	for _, r := range tree.realms {
		for _, other := range tree.realms {
			if other != r {
				r.deps = append(r.deps, other.deps[0])
			}
		}
	}
	l.realms = append(l.realms, tree.realms...)
	return nil
}

// Warnings lists the linked Standard Realms whose clause list couldn't be
// read, so their references got no figure.
func (l *Links) Warnings() []string {
	return slices.Clone(l.warnings)
}

// realmFor returns the Realm holding the entity file at path: the one whose
// directory is the longest prefix of it, so a sub-realm wins over its parent.
// A relative path is made absolute first, as the Realms' directories are.
func (l *Links) realmFor(path string) *linkRealm {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	var best *linkRealm
	for _, r := range l.realms {
		if strings.HasPrefix(path, r.dir+string(filepath.Separator)) && (best == nil || len(r.dir) > len(best.dir)) {
			best = r
		}
	}
	return best
}

// standardRealm returns the Standard Realm that the `realm` id names for a
// reference of r: the nearest dependency with that ID, read once. It is nil
// when no dependency has that ID, the nearest isn't a Standard Realm, or its
// entity files can't all be read.
func (l *Links) standardRealm(r *linkRealm, id string) *entity.StandardRealm {
	for _, deps := range r.deps {
		for _, d := range deps {
			if d.Realm == nil || d.Realm.Realm.ID != id {
				continue
			}
			if sr, ok := l.loaded[d.Dir]; ok {
				return sr
			}
			var sr *entity.StandardRealm
			if d.Realm.Realm.Kind == entity.KindStandard {
				var err error
				if sr, err = entity.LoadStandardRealm(d.Dir, d.Realm); err != nil {
					l.warnings = append(l.warnings, fmt.Sprintf("Standard Realm %s: clause list not read, so it gives no coverage figure: %v", id, err))
					sr = nil
				}
			}
			l.loaded[d.Dir] = sr
			return sr
		}
	}
	return nil
}

// exclusionKey is a standard's clause that an exclusion names.
type exclusionKey struct{ standard, clause string }

// exclusions returns the clauses the `exclusion` Realm Notes of rf name
// (STDREF-022). A ref that isn't <id>:<clause> is RM023's and excludes
// nothing.
func exclusions(rf *entity.RealmFile, into map[exclusionKey]bool) map[exclusionKey]bool {
	if into == nil {
		into = make(map[exclusionKey]bool)
	}
	for _, n := range rf.Notes {
		if n.Type != entity.NoteTypeExclusion {
			continue
		}
		if id, clause, ok := strings.Cut(n.Ref, ":"); ok && id != "" && clause != "" {
			into[exclusionKey{id, clause}] = true
		}
	}
	return into
}

// clauseList returns the clauses of sr, narrowed to those whose rules declare
// profile when it is set (STDREF-020), sorted.
func clauseList(sr *entity.StandardRealm, profile string) []string {
	var out []string
	for c, profiles := range sr.Clauses {
		if profile == "" || slices.Contains(profiles, profile) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

// realmFigureKey identifies a Realm figure: a reference in one tree, as the
// manifest that declares it holds it, nil for one an entity declares, and the
// Standard Realm it was measured against. A sub-realm that declares a
// reference with the same id has a reference of its own, with its own
// exclusions and figure, as strspc-rules#177 clarifies STDREF-025.
type realmFigureKey struct {
	tree *linkTree
	decl *linkRealm
	ref  entity.StandardRef
	sr   *entity.StandardRealm
}

// declaring returns the Realm whose manifest declares the reference id
// resolves to for the entity hop of lr, or nil when an entity, or a manifest
// outside the links, declares it. The scopes of an entity are its own, those
// of the entities holding it, then the manifests' (NTE-016): its Realm's and,
// for a sub-realm, its parent's, last. The nearest scope that holds id is the
// declaring one, and a manifest's scope is told by its position, so an entity
// whose references equal a manifest's is still an entity.
func declaring(lr *linkRealm, hop inherit.Hop, id string) *linkRealm {
	manifests := []*linkRealm{lr}
	if root := lr.tree.realms[0]; root != lr {
		manifests = append(manifests, root)
	} else if lr.tree.parent != nil {
		manifests = append(manifests, lr.tree.parent)
	}
	if entity.FindStandard(hop.Entity.Entity.Standards, id) != nil {
		return nil
	}
	// The manifests' scopes are the last of hop.Scopes, in that order. A
	// hop built with fewer, as a test may, has the first of them.
	first := max(len(hop.Scopes)-len(manifests), 0)
	for i, scope := range hop.Scopes {
		if entity.FindStandard(scope, id) == nil {
			continue
		}
		if i >= first && i-first < len(manifests) {
			return manifests[i-first]
		}
		return nil
	}
	return nil
}

// realmFigure gathers what a Realm figure counts across a tree's entities.
type realmFigure struct {
	treeID  string
	sr      *entity.StandardRealm
	covered map[string]bool
}

// linkEntity adds what the links say of one entity measured: for each
// Standard Reference in its scopes with a `realm` and the relation
// `implements` (STDREF-026), the clauses of the linked list it doesn't map,
// as excluded or unmapped (STDREF-022, STDREF-023), and its figure
// (STDREF-024). A clause is excluded for a reference when the manifest that
// declares the reference excludes it (STDREF-022), so a reference an entity
// declares excludes nothing. A mapped clause that is excluded shows as
// excluded, its mappings kept, and isn't counted covered. claims and lapsed
// are the entity's `implements` clauses.
func (r *Report) linkEntity(c *computer, e *realmresolve.Inheritance, claims, lapsed map[clauseKey]*Clause) {
	lr := c.links.realmFor(e.Path)
	if lr == nil {
		return
	}
	hop := e.Entity
	euid := hop.Entity.Entity.ID
	scopes := append([][]entity.StandardRef{hop.Entity.Entity.Standards}, hop.Scopes...)
	seen := make(map[string]bool)
	for _, scope := range scopes {
		for _, s := range scope {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			ref := entity.ResolveStandard(s.ID, scopes...)
			if ref == nil || ref.Realm == "" || ref.Relation != entity.RelationImplements {
				continue
			}
			// A manifest's reference resolves its `realm` against the
			// dependencies of the Realm declaring it, so every entity
			// measured against it reads one clause list. One an entity
			// declares, or the unresolved parent's, resolves against the
			// entity's Realm.
			decl := declaring(lr, hop, s.ID)
			from := lr
			if decl != nil && len(decl.deps) > 0 {
				from = decl
			}
			sr := c.links.standardRealm(from, ref.Realm)
			if sr == nil {
				continue
			}
			var excluded map[exclusionKey]bool
			if decl != nil {
				excluded = exclusions(decl.rf, nil)
			}
			fig := Figure{Tree: hop.RealmID, Entity: euid, Standard: ref.ID, Version: ref.Version, Realm: ref.Realm, Profile: ref.Profile}
			agg := c.realmFigure(realmFigureKey{lr.tree, decl, *ref, sr}, hop.RealmID, sr)
			list := clauseList(sr, ref.Profile)
			for _, clause := range list {
				key := clauseKey{ref.ID, ref.Version, clause}
				isExcluded := excluded[exclusionKey{ref.ID, clause}]
				if isExcluded {
					fig.Excluded++
				} else {
					fig.Total++
				}
				if cl, ok := claims[key]; ok && cl.Status == StatusCovered {
					agg.covered[clause] = true
					if !isExcluded {
						fig.Covered++
					}
				}
				if _, ok := claims[key]; ok {
					continue
				}
				if _, ok := lapsed[key]; ok {
					continue
				}
				status := StatusUnmapped
				if isExcluded {
					status = StatusExcluded
				}
				r.Clauses = append(r.Clauses, Clause{Standard: ref.ID, Version: ref.Version, Clause: clause, Entity: euid, Status: status})
			}
			fig.Defined = fig.Total > 0
			r.Figures = append(r.Figures, fig)
			// A clause of the list that the declaring manifest excludes is
			// excluded, whatever maps it. A mapping outside the list, such
			// as one above the profile, keeps its status.
			for _, clause := range list {
				if !excluded[exclusionKey{ref.ID, clause}] {
					continue
				}
				key := clauseKey{ref.ID, ref.Version, clause}
				for _, set := range []map[clauseKey]*Clause{claims, lapsed} {
					if cl, ok := set[key]; ok {
						cl.Status = StatusExcluded
					}
				}
			}
		}
	}
}

// realmFigure returns the Realm figure being gathered for key.
func (c *computer) realmFigure(key realmFigureKey, treeID string, sr *entity.StandardRealm) *realmFigure {
	if f, ok := c.realmFigures[key]; ok {
		return f
	}
	f := &realmFigure{treeID: treeID, sr: sr, covered: make(map[string]bool)}
	c.realmFigures[key] = f
	c.realmOrder = append(c.realmOrder, key)
	return f
}

// addRealmFigures adds the Realm figure of each reference measured: a clause
// counts as covered when any entity of the tree measured against the
// reference covers it (STDREF-025), and as excluded when the manifest that
// declares the reference excludes it, as strspc-rules#177 clarifies STDREF-025.
func (r *Report) addRealmFigures(c *computer) {
	for _, key := range c.realmOrder {
		agg := c.realmFigures[key]
		var excluded map[exclusionKey]bool
		if key.decl != nil {
			excluded = exclusions(key.decl.rf, nil)
		}
		fig := Figure{Tree: agg.treeID, Standard: key.ref.ID, Version: key.ref.Version, Realm: key.ref.Realm, Profile: key.ref.Profile}
		for _, clause := range clauseList(agg.sr, key.ref.Profile) {
			if excluded[exclusionKey{key.ref.ID, clause}] {
				fig.Excluded++
				continue
			}
			fig.Total++
			if agg.covered[clause] {
				fig.Covered++
			}
		}
		fig.Defined = fig.Total > 0
		r.Figures = append(r.Figures, fig)
	}
}

// sortFigures sorts the figures by tree, reference and entity, a Realm figure
// before those of its entities.
func sortFigures(figs []Figure) {
	slices.SortFunc(figs, func(a, b Figure) int {
		return cmp.Or(cmp.Compare(a.Tree, b.Tree), cmp.Compare(a.Standard, b.Standard), cmp.Compare(a.Version, b.Version),
			cmp.Compare(a.Realm, b.Realm), cmp.Compare(a.Profile, b.Profile), cmp.Compare(a.Entity, b.Entity))
	})
}
