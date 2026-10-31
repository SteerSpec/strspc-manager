package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A size near MaxInt64 must not wrap the running total past the cap check.
func TestExtractTarball_HugeSizeDoesNotOverflowCap(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "p/a", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644, Format: tar.FormatGNU}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "p/b", Typeflag: tar.TypeReg, Size: math.MaxInt64, Mode: 0o644, Format: tar.FormatGNU}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("y")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	_, err := extractTarball(&buf, "", dest)
	if err == nil || !strings.Contains(err.Error(), "extraction cap") {
		t.Fatalf("err = %v, want the extraction cap error", err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "b")); !os.IsNotExist(statErr) {
		t.Errorf("the oversized entry was written: stat err = %v", statErr)
	}
}
