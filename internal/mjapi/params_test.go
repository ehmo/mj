package mjapi

import "testing"

func iptr(i int) *int         { return &i }
func i64ptr(i int64) *int64   { return &i }
func fptr(f float64) *float64 { return &f }

func TestParamsString(t *testing.T) {
	tests := []struct {
		name string
		p    Params
		want string
	}{
		{"empty", Params{}, ""},
		{"ar+version", Params{AR: "3:2", Version: "7"}, "--ar 3:2 --v 7"},
		{"v8.1", Params{Version: "8.1"}, "--v 8.1"},
		{"niji", Params{Version: "niji 6"}, "--niji 6"},
		{"niji nospace", Params{Version: "niji7"}, "--niji 7"},
		{"numerics", Params{Stylize: iptr(500), Chaos: iptr(12), Weird: iptr(250), Quality: fptr(2), Seed: i64ptr(42), Stop: iptr(80)},
			"--s 500 --chaos 12 --weird 250 --q 2 --seed 42 --stop 80"},
		{"quality fraction", Params{Quality: fptr(0.25)}, "--q 0.25"},
		{"no csv", Params{No: []string{"trees", "cars"}}, "--no trees,cars"},
		{"tile+styleraw", Params{Tile: true, StyleRaw: true}, "--tile --style raw"},
		{"sref+sw+sv", Params{Sref: []string{"http://a", "code123"}, Sw: iptr(300), Sv: iptr(4)},
			"--sref http://a --sref code123 --sw 300 --sv 4"},
		{"oref+ow", Params{Oref: []string{"http://o"}, Ow: iptr(600)}, "--oref http://o --ow 600"},
		{"cref+cw", Params{Cref: []string{"http://c"}, Cw: iptr(50)}, "--cref http://c --cw 50"},
		{"profile", Params{Profile: "uwg8uie"}, "--profile uwg8uie"},
		{"raw appended last", Params{AR: "1:1", Raw: "--exp 20"}, "--ar 1:1 --exp 20"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	p := Params{AR: "16:9", Version: "7"}
	if got := p.BuildPrompt("a cat"); got != "a cat --ar 16:9 --v 7" {
		t.Errorf("got %q", got)
	}
	if got := (Params{}).BuildPrompt("a cat"); got != "a cat" {
		t.Errorf("no-params got %q", got)
	}
	if got := p.BuildPrompt("  spaced  "); got != "spaced --ar 16:9 --v 7" {
		t.Errorf("trim got %q", got)
	}
}

func TestParamsValidate(t *testing.T) {
	bad := []struct {
		name string
		p    Params
	}{
		{"ar format", Params{AR: "16x9"}},
		{"version", Params{Version: "5"}},
		{"stylize hi", Params{Stylize: iptr(1001)}},
		{"chaos hi", Params{Chaos: iptr(101)}},
		{"weird hi", Params{Weird: iptr(3001)}},
		{"quality", Params{Quality: fptr(3)}},
		{"seed hi", Params{Seed: i64ptr(4294967296)}},
		{"stop lo", Params{Stop: iptr(9)}},
		{"sw hi", Params{Sw: iptr(1001)}},
		{"sv hi", Params{Sv: iptr(7)}},
		{"ow hi", Params{Ow: iptr(1001)}},
		{"cw hi", Params{Cw: iptr(101)}},
	}
	for _, tc := range bad {
		t.Run("bad/"+tc.name, func(t *testing.T) {
			if _, err := tc.p.Validate(); err == nil {
				t.Errorf("expected error for %+v", tc.p)
			}
		})
	}

	good := Params{AR: "3:2", Version: "8.1", Stylize: iptr(0), Chaos: iptr(100), Quality: fptr(0.5),
		Seed: i64ptr(4294967295), Stop: iptr(10), Sw: iptr(1000), Sv: iptr(1), Cw: iptr(0)}
	if _, err := good.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRawCollisionWarning(t *testing.T) {
	p := Params{Stylize: iptr(200), Raw: "--s 500 --exp 10"}
	warns, err := p.Validate()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("want 1 warning, got %d: %v", len(warns), warns)
	}
}

func TestModeValid(t *testing.T) {
	for _, m := range []Mode{ModeFast, ModeRelax, ModeTurbo} {
		if !m.Valid() {
			t.Errorf("%s should be valid", m)
		}
	}
	if Mode("hyper").Valid() {
		t.Error("hyper should be invalid")
	}
}

func TestAspectRatio(t *testing.T) {
	cases := map[[2]int]string{
		{960, 1248}:  "10:13", // captured zoom-out canvas
		{1024, 1024}: "1:1",
		{1920, 1080}: "16:9",
		{0, 5}:       "1:1",
	}
	for in, want := range cases {
		if got := AspectRatio(in[0], in[1]); got != want {
			t.Errorf("AspectRatio(%d,%d)=%q want %q", in[0], in[1], got, want)
		}
	}
}
