package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Contributor is an entry of a Realm's contributor list (CTRB-004).
type Contributor struct {
	// ID is the contributor identifier `<idp>:<subject>` (CTRB-001), such as
	// `github:pleberre`.
	ID string `json:"id"`
	// Email is the contributor's email address (CTRB-007). Empty means none.
	Email string `json:"email,omitempty"`
	// GitHubID is the numeric GitHub user ID of a `github` contributor
	// (CTRB-008). Zero means none; the schema requires at least 1.
	GitHubID int64 `json:"github_id,omitempty"`
}

// IdentityProviderGitHub is the `<idp>` of a GitHub account.
const IdentityProviderGitHub = "github"

// identityProviders lists the identity providers core lists (CTRB-002/01),
// each with the pattern its subject must match.
var identityProviders = map[string]*regexp.Regexp{
	IdentityProviderGitHub: regexp.MustCompile(`^` + githubLogin + `$`),
}

// githubLogin matches a GitHub login, a bot's included: hyphens only between
// alphanumerics.
const githubLogin = `[A-Za-z0-9]+(-[A-Za-z0-9]+)*(\[bot\])?`

// IdentityProviders returns the identity providers core lists (CTRB-002),
// sorted.
func IdentityProviders() []string {
	out := make([]string, 0, len(identityProviders))
	for idp := range identityProviders {
		out = append(out, idp)
	}
	slices.Sort(out)
	return out
}

// ParseContributorID splits a contributor identifier `<idp>:<subject>`
// (CTRB-001). The `<idp>` must be one of IdentityProviders (CTRB-002) and the
// subject must be valid at that provider: a GitHub login for `github`.
func ParseContributorID(s string) (idp, subject string, err error) {
	idp, subject, ok := strings.Cut(s, ":")
	if !ok || idp == "" || subject == "" {
		return "", "", fmt.Errorf("contributor identifier %q is not <idp>:<subject> (CTRB-001)", s)
	}
	re, listed := identityProviders[idp]
	if !listed {
		return "", "", fmt.Errorf("contributor identifier %q names identity provider %q, which core does not list; listed: %s (CTRB-002)", s, idp, strings.Join(IdentityProviders(), ", "))
	}
	if !re.MatchString(subject) {
		return "", "", fmt.Errorf("contributor identifier %q has an invalid %s subject %q (CTRB-001)", s, idp, subject)
	}
	return idp, subject, nil
}

// ValidateContributors returns one error for each entry of a Realm's
// contributor list whose id is not a contributor identifier (CTRB-001,
// CTRB-002), and for each id repeated, in list order. Ids are compared as
// FindContributor matches them, so `github:Alice` repeats `github:alice`.
func ValidateContributors(list []Contributor) []error {
	var errs []error
	for i, c := range list {
		if _, _, err := ParseContributorID(c.ID); err != nil {
			errs = append(errs, fmt.Errorf("contributors[%d]: %w", i, err))
			continue
		}
		if prev := FindContributor(list[:i], c.ID); prev != nil {
			errs = append(errs, fmt.Errorf("contributors[%d]: id %q repeats %q", i, c.ID, prev.ID))
		}
	}
	return errs
}

// IsContributorID reports whether s is a contributor identifier with a listed
// identity provider (CTRB-001, CTRB-002).
func IsContributorID(s string) bool {
	_, _, err := ParseContributorID(s)
	return err == nil
}

// FindContributor returns the entry of list that addedBy names (CTRB-005), or
// nil. addedBy must be a contributor identifier, and it names the entry with
// that id. GitHub logins match without regard to case.
func FindContributor(list []Contributor, addedBy string) *Contributor {
	idp, subject, err := ParseContributorID(addedBy)
	if err != nil {
		return nil
	}
	for i := range list {
		eIdp, eSubject, ok := strings.Cut(list[i].ID, ":")
		if !ok || eIdp != idp {
			continue
		}
		if eSubject == subject || (idp == IdentityProviderGitHub && strings.EqualFold(eSubject, subject)) {
			return &list[i]
		}
	}
	return nil
}

// RealmContributors returns the contributors of the Realm whose manifest is
// in dir. A sub-realm uses those of its parent Realm (CTRB-006): the manifest
// in the directory above, when it lists dir as a sub-realm and its ID makes
// dir's that sub-realm's (RLM-011). A directory without a manifest has none,
// and no error. A manifest that can't be read or parsed is an error, the one
// in the directory above included, since it may be the parent's.
func RealmContributors(dir string) ([]Contributor, error) {
	rf, err := LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	parentDir, base := filepath.Dir(abs), filepath.Base(abs)
	parent, err := LoadRealm(filepath.Join(parentDir, "realm.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return rf.Realm.Contributors, nil
	case err != nil:
		return nil, fmt.Errorf("loading the manifest above %s: %w", dir, err)
	}
	if slices.Contains(parent.SubRealms, base) && parent.Realm.ID+"."+base == rf.Realm.ID {
		return RealmContributors(parentDir)
	}
	return rf.Realm.Contributors, nil
}
