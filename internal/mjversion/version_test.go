package mjversion

import (
	"runtime/debug"
	"testing"
)

func TestEffectiveUsesLinkedVersion(t *testing.T) {
	if got := Effective("0.11.0"); got != "0.11.0" {
		t.Fatalf("Effective linked version = %q, want 0.11.0", got)
	}
}

func TestEffectiveFallsBackToDev(t *testing.T) {
	if got := Effective(""); got != "dev" {
		t.Fatalf("Effective empty version = %q, want dev", got)
	}
}

func TestFromBuildInfoUsesMainVersion(t *testing.T) {
	bi := &debug.BuildInfo{Main: debug.Module{Version: "v0.11.0"}}
	if got := fromBuildInfo(bi); got != "0.11.0" {
		t.Fatalf("fromBuildInfo main version = %q, want 0.11.0", got)
	}
}

func TestFromBuildInfoUsesModuleDependencyVersion(t *testing.T) {
	bi := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Deps: []*debug.Module{{Path: "github.com/ehmo/mj", Version: "v0.11.0"}},
	}
	if got := fromBuildInfo(bi); got != "0.11.0" {
		t.Fatalf("fromBuildInfo dependency version = %q, want 0.11.0", got)
	}
}
