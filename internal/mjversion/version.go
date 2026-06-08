package mjversion

import (
	"runtime/debug"
	"strings"
)

// Effective returns the ldflags-injected version when present, otherwise the
// module version embedded by `go install module@version`.
func Effective(linked string) string {
	if linked != "" && linked != "dev" {
		return linked
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := fromBuildInfo(bi); v != "" {
			return v
		}
	}
	if linked != "" {
		return linked
	}
	return "dev"
}

func fromBuildInfo(bi *debug.BuildInfo) string {
	if bi == nil {
		return ""
	}
	if v := clean(bi.Main.Version); v != "" {
		return v
	}
	for _, dep := range bi.Deps {
		if dep.Path == "github.com/ehmo/mj" {
			if v := clean(dep.Version); v != "" {
				return v
			}
		}
	}
	return ""
}

func clean(v string) string {
	if v == "" || v == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}
