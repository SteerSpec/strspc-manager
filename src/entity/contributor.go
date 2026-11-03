package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"net/mail"
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

var (
	// handleRe matches a GitHub @handle, a bot's included.
	handleRe = regexp.MustCompile(`^@` + githubLogin + `$`)

	// domainRe matches a domain of at least two labels of up to 63 characters,
	// each without a leading or trailing hyphen. net/mail does not check this.
	domainRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$`)
)

// maxDomainLen is the longest domain name DNS allows.
const maxDomainLen = 253

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

// IsContributorID reports whether s is a contributor identifier with a listed
// identity provider (CTRB-001, CTRB-002).
func IsContributorID(s string) bool {
	_, _, err := ParseContributorID(s)
	return err == nil
}

// ValidAddedBy reports whether s is an `added_by` accepted while the
// contributor format is in transition (#248): a contributor identifier, a
// GitHub @handle or a bare email address. A display name or angle brackets
// are not bare.
func ValidAddedBy(s string) bool {
	if IsContributorID(s) || handleRe.MatchString(s) {
		return true
	}
	return isBareEmail(s)
}

func isBareEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return false
	}
	domain := s[strings.LastIndex(s, "@")+1:]
	return len(domain) <= maxDomainLen && domainRe.MatchString(domain)
}

// FindContributor returns the entry of list that addedBy names (CTRB-005), or
// nil. A contributor identifier names the entry with that id. While the
// format is in transition, `@x` names `github:x`, and an email address names
// the entry holding it. GitHub logins and email addresses match without
// regard to case.
func FindContributor(list []Contributor, addedBy string) *Contributor {
	idp, subject, err := ParseContributorID(addedBy)
	switch {
	case err == nil:
	case handleRe.MatchString(addedBy):
		idp, subject = IdentityProviderGitHub, addedBy[1:]
	case isBareEmail(addedBy):
		for i := range list {
			if list[i].Email != "" && strings.EqualFold(list[i].Email, addedBy) {
				return &list[i]
			}
		}
		return nil
	default:
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
