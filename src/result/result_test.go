package result_test

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		severity result.Severity
		want     string
	}{
		{result.Error, "error"},
		{result.Warning, "warning"},
		{result.Info, "info"},
		{result.Severity(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiagnosticString(t *testing.T) {
	tests := []struct {
		name string
		diag result.Diagnostic
		want string
	}{
		{
			name: "with path",
			diag: result.Diagnostic{Code: "RL001", Severity: result.Error, Message: "bad JSON", Path: "ENT.json"},
			want: "[error] RL001: bad JSON (ENT.json)",
		},
		{
			name: "without path",
			diag: result.Diagnostic{Code: "RE003", Severity: result.Info, Message: "skipped"},
			want: "[info] RE003: skipped",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diag.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResultOKAndErrors(t *testing.T) {
	warning := result.Diagnostic{Code: "RL012", Severity: result.Warning}
	info := result.Diagnostic{Code: "RE003", Severity: result.Info}
	first := result.Diagnostic{Code: "RL001", Severity: result.Error}
	second := result.Diagnostic{Code: "RL003", Severity: result.Error}

	tests := []struct {
		name       string
		add        []result.Diagnostic
		wantOK     bool
		wantErrors []string
	}{
		{name: "empty", wantOK: true},
		{name: "warnings and info do not fail", add: []result.Diagnostic{warning, info}, wantOK: true},
		{name: "one error fails", add: []result.Diagnostic{warning, first}, wantErrors: []string{"RL001"}},
		{name: "errors keep their order", add: []result.Diagnostic{second, info, first}, wantErrors: []string{"RL003", "RL001"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &result.Result{}
			for _, d := range tt.add {
				res.Add(d)
			}

			if len(res.Diagnostics) != len(tt.add) {
				t.Errorf("Add kept %d diagnostics, want %d", len(res.Diagnostics), len(tt.add))
			}
			if got := res.OK(); got != tt.wantOK {
				t.Errorf("OK() = %t, want %t", got, tt.wantOK)
			}
			errs := res.Errors()
			if len(errs) != len(tt.wantErrors) {
				t.Fatalf("Errors() = %v, want codes %v", errs, tt.wantErrors)
			}
			for i, code := range tt.wantErrors {
				if errs[i].Code != code {
					t.Errorf("Errors()[%d].Code = %q, want %q", i, errs[i].Code, code)
				}
			}
		})
	}
}
