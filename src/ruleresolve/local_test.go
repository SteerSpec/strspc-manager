package ruleresolve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// goodEntityDir returns a new directory holding one valid entity file (NTE).
func goodEntityDir(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(testdataPath("valid/NTE.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "NTE.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocalSource_RejectsARootThatIsNotADirectory(t *testing.T) {
	file := testdataPath("valid/NTE.json")
	missing := testdataPath("does-not-exist")

	tests := []struct {
		name    string
		ref     string
		wantMsg string
	}{
		{"file", file, "source path is not a directory: " + file},
		{"missing", missing, "source path: stat " + missing + ": no such file or directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, res := (&LocalSource{}).Fetch(context.Background(), tt.ref)
			if files != nil {
				t.Errorf("expected no files, got %d", len(files))
			}
			d := diagnostic(t, res, "RSV002")
			if d.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", d.Message, tt.wantMsg)
			}
			if d.Path != tt.ref {
				t.Errorf("path = %q, want %q", d.Path, tt.ref)
			}
		})
	}
}

func TestLocalSource_ReportsABadFileAndKeepsTheRest(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantCode string
		wantMsg  []string
	}{
		{
			name:     "empty entity id",
			content:  `{"entity": {"id": ""}, "rule_set": {"hash": null}}`,
			wantCode: "RSV003",
			wantMsg:  []string{"entity.id is empty"},
		},
		{
			// encoding/json skips the unknown field while parsing but cannot
			// hold the number in the generic form the hash is computed from.
			name:     "hash cannot be computed",
			content:  `{"entity": {"id": "NUM"}, "rule_set": {"hash": "blake3:00"}, "extra": 1e999}`,
			wantCode: "RSV004",
			wantMsg:  []string{"computing hash: unmarshaling for hash: ", "1e999"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := goodEntityDir(t)
			bad := filepath.Join(dir, "bad.json")
			if err := os.WriteFile(bad, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}

			files, res := (&LocalSource{}).Fetch(context.Background(), dir)
			d := diagnostic(t, res, tt.wantCode)
			for _, want := range tt.wantMsg {
				if !strings.Contains(d.Message, want) {
					t.Errorf("message %q does not contain %q", d.Message, want)
				}
			}
			if d.Path != bad {
				t.Errorf("path = %q, want %q", d.Path, bad)
			}
			if len(files) != 1 || files[0].File.Entity.ID != "NTE" {
				t.Errorf("expected only the good file, got %+v", files)
			}
		})
	}
}

func TestLocalSource_ReportsAnUnreadableFileAndKeepsTheRest(t *testing.T) {
	dir := goodEntityDir(t)
	link := filepath.Join(dir, "dangling.json")
	if err := os.Symlink(filepath.Join(dir, "gone"), link); err != nil {
		t.Fatal(err)
	}

	files, res := (&LocalSource{}).Fetch(context.Background(), dir)
	d := diagnostic(t, res, "RSV002")
	if !strings.HasPrefix(d.Message, "accessing path: ") || !strings.Contains(d.Message, "no such file") {
		t.Errorf("message = %q", d.Message)
	}
	if d.Path != link {
		t.Errorf("path = %q, want %q", d.Path, link)
	}
	if len(files) != 1 || files[0].File.Entity.ID != "NTE" {
		t.Errorf("expected only the good file, got %+v", files)
	}
}

func TestLocalSource_AcceptsAMatchingHash(t *testing.T) {
	unhashed := `{"entity": {"id": "OK"}, "rule_set": {"hash": null}}`
	hash, err := entity.ComputeHash([]byte(unhashed))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	hashed := strings.Replace(unhashed, "null", `"`+hash+`"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "OK.json"), []byte(hashed), 0o600); err != nil {
		t.Fatal(err)
	}

	files, res := (&LocalSource{}).Fetch(context.Background(), dir)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %v", res.Diagnostics)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestLocalSource_StopsWhenTheContextEnds(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	tests := []struct {
		name    string
		ctx     context.Context
		wantMsg string
	}{
		{"cancelled", cancelled, "walking directory: context canceled"},
		{"deadline exceeded", expired, "walking directory: context deadline exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testdataPath("valid")
			files, res := (&LocalSource{}).Fetch(tt.ctx, dir)
			if len(files) != 0 {
				t.Errorf("expected no files, got %d", len(files))
			}
			d := diagnostic(t, res, "RSV000")
			if d.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", d.Message, tt.wantMsg)
			}
			if d.Path != dir {
				t.Errorf("path = %q, want %q", d.Path, dir)
			}
			if hasCode(res, "RSV006") {
				t.Errorf("an interrupted walk says nothing about the source being empty: %v", res.Diagnostics)
			}
		})
	}
}

func TestLocalSource_ReportsAWalkFailureAsRSV002(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, res := (&LocalSource{}).Fetch(context.Background(), dir)
	d := diagnostic(t, res, "RSV002")
	if !strings.HasPrefix(d.Message, "walking directory: ") || !strings.Contains(d.Message, "permission denied") {
		t.Errorf("message = %q", d.Message)
	}
	if d.Path != dir {
		t.Errorf("path = %q, want %q", d.Path, dir)
	}
	if d.Severity != result.Error {
		t.Errorf("severity = %v, want error", d.Severity)
	}
}
