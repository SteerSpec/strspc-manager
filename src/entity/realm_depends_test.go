package entity_test

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestRealmFileDependsOn(t *testing.T) {
	rf := &entity.RealmFile{Dependencies: []entity.RealmDep{{RealmID: "dev.steerspec.core", Version: "1.15.0"}}}
	if !rf.DependsOn("dev.steerspec.core") {
		t.Error("DependsOn(core) = false, want true")
	}
	if rf.DependsOn("dev.example.other") {
		t.Error("DependsOn(other) = true, want false")
	}
	if (&entity.RealmFile{}).DependsOn("dev.steerspec.core") {
		t.Error("a manifest without dependencies depends on core")
	}
}
