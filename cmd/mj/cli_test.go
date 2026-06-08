package main

import (
	"flag"
	"fmt"
	"testing"

	"github.com/ehmo/mj/internal/mjclient"
)

func TestParseArgsMixedOrder(t *testing.T) {
	// flags after the positional
	fs := flag.NewFlagSet("vary", flag.ContinueOnError)
	idx := fs.Int("index", 0, "")
	pos := parseArgs(fs, []string{"JOB123", "--index", "2"})
	if len(pos) != 1 || pos[0] != "JOB123" {
		t.Fatalf("trailing-flag positional: %v", pos)
	}
	if *idx != 2 {
		t.Fatalf("index not parsed: %d", *idx)
	}

	// flags before the positional
	fs2 := flag.NewFlagSet("vary", flag.ContinueOnError)
	idx2 := fs2.Int("index", 0, "")
	pos2 := parseArgs(fs2, []string{"--index", "3", "JOB456"})
	if len(pos2) != 1 || pos2[0] != "JOB456" {
		t.Fatalf("leading-flag positional: %v", pos2)
	}
	if *idx2 != 3 {
		t.Fatalf("index not parsed: %d", *idx2)
	}

	// multi-word positional prompt with a trailing flag
	fs3 := flag.NewFlagSet("imagine", flag.ContinueOnError)
	ar := fs3.String("ar", "", "")
	pos3 := parseArgs(fs3, []string{"a", "b", "c", "--ar", "3:2"})
	if got := fmt.Sprint(pos3); got != "[a b c]" {
		t.Fatalf("multiword positionals: %v", pos3)
	}
	if *ar != "3:2" {
		t.Fatalf("ar not parsed: %q", *ar)
	}
}

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{mjclient.ErrWaitTimeout, 5},
		{mjclient.ErrJobFailed, 4},
		{&mjclient.AuthError{Reason: "x"}, 3},
		{&mjclient.APIError{Status: 403}, 6},
		{&mjclient.APIError{Status: 500}, 1},
		{fmt.Errorf("plain"), 1},
	}
	for _, tc := range cases {
		if got := exitCode(tc.err); got != tc.want {
			t.Errorf("exitCode(%v)=%d want %d", tc.err, got, tc.want)
		}
	}
}

func TestPrintCommandHelp(t *testing.T) {
	if !printCommandHelp("imagine") {
		t.Errorf("imagine should be a known help topic")
	}
	if !printCommandHelp("--download") { // leading dashes tolerated
		// note: "download" is a topic; "--download" trims to "download"
		t.Errorf("--download should resolve to download topic")
	}
	if printCommandHelp("definitely-not-a-command") {
		t.Errorf("unknown topic should return false")
	}
}
