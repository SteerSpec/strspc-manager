package entity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRealmDepSourceUnmarshal(t *testing.T) {
	input := `{
		"realm_id": "dev.steerspec.core",
		"version": "0.1.0",
		"source": "github://SteerSpec/strspc-rules@latest/rules/core"
	}`
	var dep RealmDep
	if err := json.Unmarshal([]byte(input), &dep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dep.Source != "github://SteerSpec/strspc-rules@latest/rules/core" {
		t.Errorf("Source = %q, want %q", dep.Source, "github://SteerSpec/strspc-rules@latest/rules/core")
	}
}

func TestRealmDepSourceOmitEmpty(t *testing.T) {
	dep := RealmDep{RealmID: "dev.steerspec.core", Version: "0.1.0"}
	data, err := json.Marshal(dep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "source") {
		t.Errorf("expected source to be omitted, got: %s", data)
	}
}
