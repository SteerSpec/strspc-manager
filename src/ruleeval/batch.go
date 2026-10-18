package ruleeval

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"lukechampine.com/blake3"
)

// ErrUnavailable is returned (wrapped) by providers when the backend cannot
// be used at all: missing or rejected credentials, unknown model, or an
// unreachable endpoint. Callers should stop evaluating rather than retry.
var ErrUnavailable = errors.New("ruleeval: provider unavailable")

// Location points at a place in the diff that supports a verdict.
type Location struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Excerpt string `json:"excerpt,omitempty"`
}

// BatchRequest asks a provider to evaluate several rules of one entity
// against the same diff in a single round trip.
type BatchRequest struct {
	EntityID string
	Inputs   []RuleInput
	Diff     string
}

// BatchResponse carries one verdict per requested rule ID. Errors holds
// non-fatal partial failures (for example a diff hunk too large to send)
// that the evaluator reports as RE002 warnings without discarding the
// verdicts that were obtained.
type BatchResponse struct {
	Verdicts map[string]*EvalResponse
	Errors   []error
}

// BatchProvider is an optional capability of a Provider. When the provider
// passed to New implements it, the Evaluator sends one request per entity
// instead of one per rule.
type BatchProvider interface {
	EvaluateBatch(ctx context.Context, req BatchRequest) (*BatchResponse, error)
}

// Cache stores evaluation responses keyed by CacheKey. Implementations must
// be safe for concurrent use. Errors are reported to the Observer and never
// abort evaluation.
type Cache interface {
	Get(key string) (resp *EvalResponse, ok bool, err error)
	Put(key string, resp *EvalResponse) error
}

// Usage accumulates token and request counts across provider calls.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Requests         int
}

// Add accumulates o into u.
func (u *Usage) Add(o Usage) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.Requests += o.Requests
}

// UsageReporter is an optional capability of a Provider that tracks usage.
type UsageReporter interface {
	Usage() Usage
}

// Describer is an optional capability of a Provider that identifies the
// model and prompt version in use. Both values are mixed into cache keys so
// that a model or prompt change never serves stale verdicts.
type Describer interface {
	Model() string
	PromptVersion() string
}

// EventKind classifies Observer events.
type EventKind int

// Event kinds emitted by the Evaluator.
const (
	EventCacheHit     EventKind = iota // RuleID set
	EventCacheMiss                     // RuleID set
	EventCacheError                    // RuleID and Err set
	EventProviderCall                  // EntityID, RuleIDs and (on failure) Err set
	EventSkipped                       // RuleID set; rule state not in evaluate set
)

// Event describes something the Evaluator did.
type Event struct {
	Kind     EventKind
	EntityID string
	RuleID   string
	RuleIDs  []string
	Err      error
}

// Observer receives Events as evaluation progresses. Implementations must
// be safe for concurrent use.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to the Observer interface.
type ObserverFunc func(Event)

// Observe calls f.
func (f ObserverFunc) Observe(ev Event) { f(ev) }

// DefaultEvaluateStates are the rule states evaluated when
// WithEvaluateStates is not given: Draft, Published and Implemented.
var DefaultEvaluateStates = []string{"D", "P", "I"}

// CacheKey derives the cache key for one rule evaluated against one diff.
// It is the hex-encoded BLAKE3 hash of the rule ID, revision, body, notes
// (type and content, order-independent), source and scope together with
// diffHash, the model and the prompt version. Source and scope are part of
// the prompt (a local rule applies only under its source directory), so a
// scope change must invalidate a cached not_applicable verdict. Rule state
// is deliberately excluded: it drives severity, not the verdict.
func CacheKey(rule RuleInput, diffHash, model, promptVersion string) string {
	notes := make([]string, 0, len(rule.Notes))
	for _, n := range rule.Notes {
		notes = append(notes, n.Type+"\x1f"+n.Content)
	}
	sort.Strings(notes)

	var b strings.Builder
	fields := []string{
		rule.Rule.ID,
		strconv.Itoa(rule.Rule.Revision),
		rule.Rule.Body,
		strings.Join(notes, "\x1e"),
		rule.Source,
		rule.Scope,
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

// DiffHash returns the hex-encoded BLAKE3 hash of a diff.
func DiffHash(diff string) string {
	sum := blake3.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])
}
