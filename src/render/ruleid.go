package render

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// storedRuleIDRe matches the stored "<EUID>-<NNN>" form of a rule ID.
var storedRuleIDRe = regexp.MustCompile(`^([A-Za-z0-9]{3,18})-(\d{3})$`)

// RuleIDFormatter formats a rule's display identifier according to RLIFRMT rules.
// It changes only the rendered form, never the stored rule ID (RLIFRMT-008).
type RuleIDFormatter struct {
	// OpenBracket is written before the rule ID.
	OpenBracket string
	// CloseBracket is written after the state code.
	CloseBracket string
	// IDSplitter separates the EUID and the Rule Set Unique Number; empty
	// keeps the stored "-".
	IDSplitter string
	// EUIDClosing puts the Rule Set Unique Number first and the EUID last.
	EUIDClosing bool
	// RevisionSplitter separates the rule ID from the revision number.
	RevisionSplitter string
	// StateSplitter separates the revision number from the state code.
	StateSplitter string
}

// DefaultRuleIDFormatter returns a formatter with RLIFRMT default settings.
func DefaultRuleIDFormatter() RuleIDFormatter {
	return RuleIDFormatter{
		OpenBracket:      "[",
		CloseBracket:     "]",
		IDSplitter:       "-",
		RevisionSplitter: ".",
		StateSplitter:    "/",
	}
}

// NewRuleIDFormatter returns the formatter for a Realm's rule_identifier_format.
// A nil format, or an empty field, keeps the RLIFRMT default. Values outside
// the realm.v1 schema enums are an error. For an entity of a sub-realm, pass
// the parent Realm's format (RLM-015).
func NewRuleIDFormatter(rif *entity.RuleIdentifierFormat) (RuleIDFormatter, error) {
	f := DefaultRuleIDFormatter()
	if rif == nil {
		return f, nil
	}
	if rif.Brackets != "" {
		if err := checkEnum("brackets", rif.Brackets, "(,)", "{,}", "[,]"); err != nil {
			return f, err
		}
		f.OpenBracket, f.CloseBracket, _ = strings.Cut(rif.Brackets, ",")
	}
	if rif.EUIDPosition != "" {
		if err := checkEnum("euid_position", rif.EUIDPosition, "opening", "closing"); err != nil {
			return f, err
		}
		f.EUIDClosing = rif.EUIDPosition == "closing"
	}
	if rif.IDSplitter != "" {
		if err := checkEnum("id_splitter", rif.IDSplitter, "-", "_"); err != nil {
			return f, err
		}
		f.IDSplitter = rif.IDSplitter
	}
	if rif.RevisionSplitter != "" {
		if err := checkEnum("revision_splitter", rif.RevisionSplitter, ".", "#"); err != nil {
			return f, err
		}
		f.RevisionSplitter = rif.RevisionSplitter
	}
	if rif.StateSplitter != "" {
		if err := checkEnum("state_splitter", rif.StateSplitter, "/", `\`, "|"); err != nil {
			return f, err
		}
		f.StateSplitter = rif.StateSplitter
	}
	return f, nil
}

func checkEnum(field, value string, allowed ...string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("rule_identifier_format.%s %q: must be one of %s", field, value, strings.Join(allowed, " "))
}

// Format returns the rendered rule identifier, e.g. "[ENT-001.0/D]". A rule
// ID not in the stored "<EUID>-<NNN>" form is written as is.
func (f RuleIDFormatter) Format(r *entity.Rule) string {
	return fmt.Sprintf("%s%s%s%d%s%s%s",
		f.OpenBracket,
		f.formatID(r.ID),
		f.RevisionSplitter,
		r.Revision,
		f.StateSplitter,
		r.State,
		f.CloseBracket,
	)
}

func (f RuleIDFormatter) formatID(id string) string {
	m := storedRuleIDRe.FindStringSubmatch(id)
	if m == nil {
		return id
	}
	sep := f.IDSplitter
	if sep == "" {
		sep = "-"
	}
	if f.EUIDClosing {
		return m[2] + sep + m[1]
	}
	return m[1] + sep + m[2]
}
