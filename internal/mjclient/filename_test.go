package mjclient

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/mj/internal/mjapi"
)

func TestSafeIDBase(t *testing.T) {
	// Exact, stable mappings.
	exact := map[string]string{
		"abc123-DEF_4.5": "abc123-DEF_4.5", // legitimate UUID-ish: unchanged
		"a/b\\c":         "a_b_c",          // separators neutralized
		"":               "job",            // empty -> placeholder
	}
	for in, want := range exact {
		if got := safeIDBase(mjapi.JobID(in)); got != want {
			t.Errorf("safeIDBase(%q)=%q want %q", in, got, want)
		}
	}
	// Security invariants for hostile inputs (exact form not important).
	hostile := []string{"../../etc/passwd", "....//....//x", "/abs/path", "..", "...", "a\x00b"}
	for _, in := range hostile {
		got := safeIDBase(mjapi.JobID(in))
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("safeIDBase(%q)=%q contains a path separator", in, got)
		}
		if strings.HasPrefix(got, ".") {
			t.Errorf("safeIDBase(%q)=%q starts with a dot", in, got)
		}
		if got == "" {
			t.Errorf("safeIDBase(%q) is empty", in)
		}
	}
}

// A hostile job id must not let an asset filename escape the destination dir.
func TestAssetFilenameNoTraversal(t *testing.T) {
	id := mjapi.JobID("../../../../tmp/evil")
	a := mjapi.AssetURL{Kind: mjapi.AssetCell, Index: 0, URL: "https://cdn/x/0_0.png"}
	name := assetFilename(id, a)
	if strings.ContainsAny(name, `/\`) {
		t.Fatalf("filename %q contains a separator", name)
	}
	dest := filepath.Join("/safe/dir", name)
	if !strings.HasPrefix(dest, "/safe/dir/") {
		t.Fatalf("join escaped the dir: %q", dest)
	}
}
