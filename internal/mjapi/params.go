package mjapi

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// AspectRatio reduces a width and height to a "W:H" aspect-ratio string (e.g.
// 960x1248 -> "10:13"). Returns "1:1" for non-positive inputs.
func AspectRatio(w, h int) string {
	if w <= 0 || h <= 0 {
		return "1:1"
	}
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	return fmt.Sprintf("%d:%d", w/a, h/a)
}

// Params are the prompt-suffix generation parameters (spec §12). Mode and
// stealth are NOT here — they go in the submit-jobs `f` envelope. Optional
// numeric fields are pointers so "unset" is distinct from a zero value.
type Params struct {
	AR       string   // "W:H"
	Version  string   // "7", "8", "8.1", "niji 6", "niji 7"
	Stylize  *int     // 0..1000
	Chaos    *int     // 0..100
	Weird    *int     // 0..3000
	Quality  *float64 // 0.25/0.5/1/2/4
	Seed     *int64   // 0..4294967295
	No       []string // negative terms
	Tile     bool
	Stop     *int     // 10..100
	Sref     []string // urls or codes
	Sw       *int     // 0..1000
	Sv       *int     // 1..6
	Oref     []string // urls
	Ow       *int     // 0..1000
	Cref     []string // urls
	Cw       *int     // 0..100
	Profile  string   // moodboard/profile id
	StyleRaw bool
	Raw      string // verbatim passthrough, appended last
}

var (
	arRE      = regexp.MustCompile(`^\d+:\d+$`)
	nijiRE    = regexp.MustCompile(`^niji\s*([67])$`)
	validVer  = map[string]bool{"7": true, "8": true, "8.1": true}
	validQual = map[float64]bool{0.25: true, 0.5: true, 1: true, 2: true, 4: true}
)

// Warnings returned by Validate that are non-fatal (e.g. --raw flag collisions).
type ParamWarning string

// Validate checks ranges and formats, clamping nothing (callers get errors for
// out-of-range values so mistakes surface early). It returns any non-fatal
// warnings (currently: --raw duplicating a structured flag).
func (p Params) Validate() ([]ParamWarning, error) {
	if p.AR != "" && !arRE.MatchString(p.AR) {
		return nil, fmt.Errorf("ar %q must be W:H integers", p.AR)
	}
	if p.Version != "" && !validVer[p.Version] && !nijiRE.MatchString(p.Version) {
		return nil, fmt.Errorf("version %q must be one of 7, 8, 8.1, niji 6, niji 7", p.Version)
	}
	if err := rangeInt("stylize", p.Stylize, 0, 1000); err != nil {
		return nil, err
	}
	if err := rangeInt("chaos", p.Chaos, 0, 100); err != nil {
		return nil, err
	}
	if err := rangeInt("weird", p.Weird, 0, 3000); err != nil {
		return nil, err
	}
	if p.Quality != nil && !validQual[*p.Quality] {
		return nil, fmt.Errorf("quality %v must be one of 0.25, 0.5, 1, 2, 4", *p.Quality)
	}
	if p.Seed != nil && (*p.Seed < 0 || *p.Seed > 4294967295) {
		return nil, fmt.Errorf("seed %d out of range 0..4294967295", *p.Seed)
	}
	if err := rangeInt("stop", p.Stop, 10, 100); err != nil {
		return nil, err
	}
	if err := rangeInt("sw", p.Sw, 0, 1000); err != nil {
		return nil, err
	}
	if err := rangeInt("sv", p.Sv, 1, 6); err != nil {
		return nil, err
	}
	if err := rangeInt("ow", p.Ow, 0, 1000); err != nil {
		return nil, err
	}
	if err := rangeInt("cw", p.Cw, 0, 100); err != nil {
		return nil, err
	}
	var warns []ParamWarning
	if p.Raw != "" {
		for _, f := range rawFlagTokens(p.Raw) {
			if p.structuredHas(f) {
				warns = append(warns, ParamWarning(fmt.Sprintf(
					"--raw repeats %q which is also set via a structured flag; both will be sent", f)))
			}
		}
	}
	return warns, nil
}

func rangeInt(name string, v *int, lo, hi int) error {
	if v == nil {
		return nil
	}
	if *v < lo || *v > hi {
		return fmt.Errorf("%s %d out of range %d..%d", name, *v, lo, hi)
	}
	return nil
}

// String serializes the params into the "--flag value" suffix appended to the
// prompt for submit-jobs. Order is deterministic for testability. Validate
// should be called first; String assumes valid input but is defensive.
func (p Params) String() string {
	var b []string
	add := func(parts ...string) { b = append(b, parts...) }

	if p.AR != "" {
		add("--ar", p.AR)
	}
	if p.Version != "" {
		if m := nijiRE.FindStringSubmatch(p.Version); m != nil {
			add("--niji", m[1])
		} else {
			add("--v", p.Version)
		}
	}
	if p.Stylize != nil {
		add("--s", strconv.Itoa(*p.Stylize))
	}
	if p.Chaos != nil {
		add("--chaos", strconv.Itoa(*p.Chaos))
	}
	if p.Weird != nil {
		add("--weird", strconv.Itoa(*p.Weird))
	}
	if p.Quality != nil {
		add("--q", strconv.FormatFloat(*p.Quality, 'g', -1, 64))
	}
	if p.Seed != nil {
		add("--seed", strconv.FormatInt(*p.Seed, 10))
	}
	if len(p.No) > 0 {
		add("--no", strings.Join(p.No, ","))
	}
	if p.Tile {
		add("--tile")
	}
	if p.Stop != nil {
		add("--stop", strconv.Itoa(*p.Stop))
	}
	for _, s := range p.Sref {
		add("--sref", s)
	}
	if p.Sw != nil {
		add("--sw", strconv.Itoa(*p.Sw))
	}
	if p.Sv != nil {
		add("--sv", strconv.Itoa(*p.Sv))
	}
	for _, s := range p.Oref {
		add("--oref", s)
	}
	if p.Ow != nil {
		add("--ow", strconv.Itoa(*p.Ow))
	}
	for _, s := range p.Cref {
		add("--cref", s)
	}
	if p.Cw != nil {
		add("--cw", strconv.Itoa(*p.Cw))
	}
	if p.Profile != "" {
		add("--profile", p.Profile)
	}
	if p.StyleRaw {
		add("--style", "raw")
	}
	if raw := strings.TrimSpace(p.Raw); raw != "" {
		add(raw)
	}
	return strings.Join(b, " ")
}

// BuildPrompt joins a base prompt with the serialized params.
func (p Params) BuildPrompt(prompt string) string {
	suffix := p.String()
	prompt = strings.TrimSpace(prompt)
	if suffix == "" {
		return prompt
	}
	if prompt == "" {
		return suffix
	}
	return prompt + " " + suffix
}

// structuredHas reports whether flag (e.g. "--s") is set via a structured field.
func (p Params) structuredHas(flag string) bool {
	switch flag {
	case "--ar":
		return p.AR != ""
	case "--v", "--niji":
		return p.Version != ""
	case "--s", "--stylize":
		return p.Stylize != nil
	case "--chaos":
		return p.Chaos != nil
	case "--weird", "--w":
		return p.Weird != nil
	case "--q", "--quality":
		return p.Quality != nil
	case "--seed":
		return p.Seed != nil
	case "--no":
		return len(p.No) > 0
	case "--tile":
		return p.Tile
	case "--stop":
		return p.Stop != nil
	case "--sref":
		return len(p.Sref) > 0
	case "--sw":
		return p.Sw != nil
	case "--sv":
		return p.Sv != nil
	case "--oref":
		return len(p.Oref) > 0
	case "--ow":
		return p.Ow != nil
	case "--cref":
		return len(p.Cref) > 0
	case "--cw":
		return p.Cw != nil
	case "--p", "--profile":
		return p.Profile != ""
	case "--style":
		return p.StyleRaw
	}
	return false
}

var flagTokenRE = regexp.MustCompile(`(--[a-zA-Z]+)`)

func rawFlagTokens(raw string) []string {
	return flagTokenRE.FindAllString(raw, -1)
}
