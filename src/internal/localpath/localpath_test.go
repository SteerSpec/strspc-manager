package localpath

import (
	"runtime"
	"testing"
)

func TestNonDriveVolume(t *testing.T) {
	tests := []struct {
		vol  string
		want bool
	}{
		{"", false},
		{"C:", false},
		{"z:", false},
		{`\\host\share`, true},
		{"//host/share", true},
		{`\\?\C:`, true},
		{`\\?\UNC\host\share`, true},
		{`\\.\pipe`, true},
	}
	for _, tt := range tests {
		if got := nonDriveVolume(tt.vol); got != tt.want {
			t.Errorf("nonDriveVolume(%q) = %v, want %v", tt.vol, got, tt.want)
		}
	}
}

func TestIsLocal(t *testing.T) {
	for _, p := range []string{"rules", "../core/rules", "/abs/rules", `C:\work\rules`} {
		if !IsLocal(p) {
			t.Errorf("IsLocal(%q) = false, want true", p)
		}
	}
	if runtime.GOOS != "windows" {
		return
	}
	for _, p := range []string{`\\host\share\rules`, "//host/share/rules", `\\?\C:\rules`, `\\.\pipe\x`} {
		if IsLocal(p) {
			t.Errorf("IsLocal(%q) = true, want false", p)
		}
	}
}
