// Package result defines the shared diagnostic types used by all
// strspc-manager modules (rule-lint, rule-diff, rule-eval, etc.).
package result

import "fmt"

// Severity indicates how serious a diagnostic finding is.
type Severity int

// Severity levels for diagnostic findings.
const (
	Error   Severity = iota // must fix — blocks CI
	Warning                 // should fix — does not block
	Info                    // informational
)

// String returns the severity name.
func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Info:
		return "info"
	default:
		return "unknown"
	}
}

// Diagnostic represents a single finding from any module.
type Diagnostic struct {
	Module   string   // originating module: "rule-lint", "rule-diff", etc.
	Code     string   // diagnostic code: "RL001", "RD003", etc.
	Severity Severity // error, warning, or info
	Message  string   // human-readable description
	Path     string   // file path or JSON pointer
	Line     int      // source line (0 if not applicable)
}

// String returns a formatted diagnostic message.
func (d Diagnostic) String() string {
	if d.Path != "" {
		return fmt.Sprintf("[%s] %s: %s (%s)", d.Severity, d.Code, d.Message, d.Path)
	}
	return fmt.Sprintf("[%s] %s: %s", d.Severity, d.Code, d.Message)
}

// Result aggregates diagnostics from a module run.
type Result struct {
	Diagnostics []Diagnostic
}

// OK returns true if there are no Error-severity diagnostics.
func (r *Result) OK() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == Error {
			return false
		}
	}
	return true
}

// Add appends a diagnostic to the result.
func (r *Result) Add(d Diagnostic) {
	r.Diagnostics = append(r.Diagnostics, d)
}

// Errors returns only Error-severity diagnostics.
func (r *Result) Errors() []Diagnostic {
	var errs []Diagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == Error {
			errs = append(errs, d)
		}
	}
	return errs
}
