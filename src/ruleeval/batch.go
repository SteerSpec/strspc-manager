package ruleeval

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"

	"lukechampine.com/blake3"
)

// ErrUnavailable is returned (wrapped) by providers when the backend cannot
// be used at all: missing or rejected credentials, unknown model, or an
// unreachable endpoint. Callers should stop evaluating rather than retry.
var ErrUnavailable = errors.New("ruleeval: provider unavailable")

// ErrAuditUnsupported is returned by Evaluator.Audit when the provider does
// not implement Auditor, or reports an empty audit prompt version.
var ErrAuditUnsupported = errors.New("ruleeval: provider does not support audits")

// File is one file of the code an audit judges.
type File struct {
	// Path is the file's path, as evidence cites it.
	Path string
	// Content is the file's text.
	Content string
}

// Auditor is an optional capability of a Provider: it judges the rules of
// a BatchRequest or EvalRequest whose Files are set as an audit of those
// files, not as a diff. Evaluator.Audit requires it.
type Auditor interface {
	// AuditPromptVersion identifies the audit prompt revision, which cache
	// keys mix in for audit verdicts. A wrapper around another provider
	// returns the wrapped provider's version, or "" when that provider
	// cannot audit.
	AuditPromptVersion() string
}

// Location points at a place in the diff that supports a verdict.
type Location struct {
	// File is the path of the file, as given in the diff header.
	File string `json:"file"`
	// Line is the line number in the new version of the file.
	Line int `json:"line"`
	// Excerpt is a short quote of the offending line, if any.
	Excerpt string `json:"excerpt,omitempty"`
}

// Detail says which verdicts a provider details: re-reads the diff against
// the rule, confirms or withdraws the verdict, and gives an explanation, a
// confidence and evidence locations. A violated verdict is always detailed.
// It is a floor, not a ceiling: a provider may detail more verdicts when it
// costs nothing more, as the Claude provider does for an audit too large
// for one request, whose judgement explains every verdict at once.
type Detail int

// The detail levels. not_applicable is never detailed.
const (
	// DetailViolated details violated verdicts only. It is the zero value and
	// the default, and the cheapest: most rules of a diff are compliant or
	// not applicable.
	DetailViolated Detail = iota
	// DetailCompliant also details compliant verdicts, citing the code that
	// satisfies the rule. Each compliant rule costs one more request.
	DetailCompliant
)

// Covers reports whether d details a rule judged v.
func (d Detail) Covers(v Verdict) bool {
	switch v {
	case Violated:
		return true
	case Compliant:
		return d == DetailCompliant
	default:
		return false
	}
}

// String returns the level's name, which cache keys mix in.
func (d Detail) String() string {
	switch d {
	case DetailViolated:
		return "violated"
	case DetailCompliant:
		return "compliant"
	default:
		return "Detail(" + strconv.Itoa(int(d)) + ")"
	}
}

// BatchRequest asks a provider to evaluate several rules of one entity
// against the same diff in a single round trip.
type BatchRequest struct {
	// EntityID identifies the entity whose rules are evaluated.
	EntityID string
	// Inputs lists the rules of the entity to evaluate, with their notes and
	// source metadata.
	Inputs []RuleInput
	// Diff is the unified diff every rule is evaluated against. It is empty
	// in an audit.
	Diff string
	// Files is the file set to audit instead of a diff, in the order to show
	// it; nil when a diff is evaluated. Only an Auditor reads it.
	Files []File
	// Detail says which verdicts the provider details.
	Detail Detail
	// Store keeps what a provider learns of parts of the subject, so that
	// a rerun sends only what changed: in an audit too large for one
	// request, what each file holds for each rule. It is nil when the
	// Evaluator's cache does not implement BlobStore, or for a diff. A
	// provider treats a failing Store as empty; the Evaluator reports the
	// failure to its Observer.
	Store BlobStore
}

// BlobStore is an optional capability of a Cache: it keeps opaque values
// that a provider derives and keys itself, apart from the verdicts. The
// Evaluator forwards it in BatchRequest.Store for audits. Implementations
// must be safe for concurrent use.
type BlobStore interface {
	// GetBlob returns the value stored under key; ok is false on a miss.
	GetBlob(key string) (value []byte, ok bool, err error)
	// PutBlobs stores every value under its key, at once: a provider may
	// keep thousands of values from one request, which a store writing to
	// disk should not commit one by one.
	PutBlobs(values map[string][]byte) error
}

// BatchResponse carries one verdict per requested rule ID. Errors holds
// non-fatal partial failures (for example a diff hunk too large to send)
// that the evaluator reports as RE002 warnings without discarding the
// verdicts that were obtained.
type BatchResponse struct {
	// Verdicts maps each evaluated rule ID to its response.
	Verdicts map[string]*EvalResponse
	// Errors lists non-fatal partial failures that did not prevent the verdicts
	// above.
	Errors []error
}

// BatchProvider is an optional capability of a Provider. When the provider
// passed to New implements it, the Evaluator sends one request per entity
// instead of one per rule.
type BatchProvider interface {
	// EvaluateBatch judges every rule in req against the diff in one or
	// more provider calls and returns a verdict per rule.
	EvaluateBatch(ctx context.Context, req BatchRequest) (*BatchResponse, error)
}

// Cache stores evaluation responses keyed by CacheKey. Implementations must
// be safe for concurrent use. Errors are reported to the Observer and never
// abort evaluation.
type Cache interface {
	// Get returns the response stored under key; ok is false on a miss.
	Get(key string) (resp *EvalResponse, ok bool, err error)
	// Put stores resp under key.
	Put(key string, resp *EvalResponse) error
}

// Usage accumulates token and request counts across provider calls.
type Usage struct {
	// InputTokens counts prompt tokens sent to the provider.
	InputTokens int64
	// OutputTokens counts completion tokens returned by the provider.
	OutputTokens int64
	// CacheReadTokens counts prompt tokens served from the provider's prompt
	// cache.
	CacheReadTokens int64
	// CacheWriteTokens counts prompt tokens written to the provider's prompt
	// cache.
	CacheWriteTokens int64
	// Requests counts the provider calls made.
	Requests int
	// Withdrawn counts verdicts the detail step took back after re-reading
	// the diff with the single rule in focus: violations, and with
	// DetailCompliant compliant verdicts too.
	Withdrawn int
}

// Add accumulates o into u.
func (u *Usage) Add(o Usage) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.Withdrawn += o.Withdrawn
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.Requests += o.Requests
}

// UsageReporter is an optional capability of a Provider that tracks usage.
type UsageReporter interface {
	// Usage returns the tokens and requests consumed so far.
	Usage() Usage
}

// Describer is an optional capability of a Provider that identifies the
// model and prompt version in use. Both values are mixed into cache keys so
// that a model or prompt change never serves stale verdicts.
type Describer interface {
	// Model names the model the provider sends requests to.
	Model() string
	// PromptVersion identifies the prompt revision in use.
	PromptVersion() string
}

// EventKind classifies Observer events.
type EventKind int

// Event kinds emitted by the Evaluator.
const (
	// EventCacheHit reports that a verdict was served from the cache; RuleID is
	// set.
	EventCacheHit EventKind = iota
	// EventCacheMiss reports that no cached verdict was found; RuleID is set.
	EventCacheMiss
	// EventCacheError reports a cache read or write failure; Err is set, with
	// RuleID for a verdict, or EntityID alone for what a provider stores
	// through BatchRequest.Store.
	EventCacheError
	// EventProviderCall reports a provider request; EntityID and RuleIDs are
	// set, and Err on failure.
	EventProviderCall
	// EventSkipped reports a rule left unevaluated because its state is not in
	// the evaluate set; RuleID is set.
	EventSkipped
)

// Event describes something the Evaluator did.
type Event struct {
	// Kind says what happened.
	Kind EventKind
	// EntityID is the entity concerned, when known.
	EntityID string
	// RuleID is the single rule concerned, for per-rule events.
	RuleID string
	// RuleIDs lists the rules sent in one provider call.
	RuleIDs []string
	// Err is the failure, if any.
	Err error
}

// Observer receives Events as evaluation progresses. Implementations must
// be safe for concurrent use.
type Observer interface {
	// Observe is called once per Event, possibly from several goroutines.
	Observe(Event)
}

// ObserverFunc adapts a function to the Observer interface.
type ObserverFunc func(Event)

// Observe calls f.
func (f ObserverFunc) Observe(ev Event) { f(ev) }

// DefaultEvaluateStates are the rule states evaluated when
// WithEvaluateStates is not given: Draft, Published and Implemented.
var DefaultEvaluateStates = []string{"D", "P", "I"}

// PromptNoteTypes lists the note types sent to the evaluator, in the order they
// are rendered. Other note types (changelog, reference, standard, supersedes, …)
// are bookkeeping: they are omitted from the prompt and from CacheKey, so adding
// a mapping to a standard (NTE-018) keeps a rule's cached verdicts.
var PromptNoteTypes = []string{"applies_to", "rationale", "example", "counter_example", "clarification", "extends"}

// CacheKey derives the cache key for one rule evaluated against one diff.
// It is the hex-encoded BLAKE3 hash of the rule ID, revision, body, the notes
// the prompt shows (type and content, order-independent), entity, source and
// scope, and the
// inheritance metadata (owner, inheritor, overrides, overridden-by) together
// and the extended rules (ID, revision and body, order-independent) together
// with diffHash, the model and the prompt version. The entity, source, scope
// and inheritance metadata are part of the prompt (a local rule applies only
// under its source directory; an inherited rule is judged against its
// inheritor's subject), so a change to them must invalidate a cached
// verdict. Rule state is deliberately excluded: it drives severity, not the
// verdict.
func CacheKey(rule RuleInput, diffHash, model, promptVersion string) string {
	notes := make([]string, 0, len(rule.Notes))
	for _, n := range rule.Notes {
		if slices.Contains(PromptNoteTypes, n.Type) {
			notes = append(notes, n.Type+"\x1f"+n.Content)
		}
	}
	sort.Strings(notes)
	overriddenBy := append([]string(nil), rule.OverriddenBy...)
	sort.Strings(overriddenBy)
	extended := make([]string, 0, len(rule.Extended))
	for _, e := range rule.Extended {
		extended = append(extended, e.ID+"\x1f"+strconv.Itoa(e.Revision)+"\x1f"+e.Body)
	}
	sort.Strings(extended)

	var b strings.Builder
	fields := []string{
		rule.Rule.ID,
		strconv.Itoa(rule.Rule.Revision),
		rule.Rule.Body,
		strings.Join(notes, "\x1e"),
		rule.EntityID,
		rule.Source,
		rule.Scope,
		rule.Owner,
		rule.InheritedBy,
		rule.Overrides,
		strings.Join(overriddenBy, "\x1e"),
		strings.Join(extended, "\x1e"),
		diffHash,
		model,
		promptVersion,
	}
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(strconv.Itoa(len(f)))
		b.WriteByte(':')
		b.WriteString(f)
	}
	sum := blake3.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// FilesHash returns the hex-encoded BLAKE3 hash of a file set: every path
// and content, length-prefixed, in order. The order counts, since it is the
// order the files are shown in.
func FilesHash(files []File) string {
	h := blake3.New(32, nil)
	for _, f := range files {
		for _, s := range []string{f.Path, f.Content} {
			_, _ = h.Write([]byte(strconv.Itoa(len(s)) + ":"))
			_, _ = h.Write([]byte(s))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DiffHash returns the hex-encoded BLAKE3 hash of a diff.
func DiffHash(diff string) string {
	sum := blake3.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])
}
