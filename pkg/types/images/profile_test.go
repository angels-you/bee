package images

import (
	"math"
	"testing"

	"github.com/angels-you/bee/pkg/types/code"
)

func TestGeometry(t *testing.T) {
	cases := []struct {
		name string
		p    Profile
		w, h int
		want Geometry
	}{
		{"inside down", Profile{Fit: Inside, MaxWidth: 1000, MaxHeight: 500}, 4000, 3000, Geometry{667, 500, 667, 500}},
		{"inside width only", Profile{Fit: Inside, MaxWidth: 1000}, 4000, 3000, Geometry{1000, 750, 1000, 750}},
		{"inside height only", Profile{Fit: Inside, MaxHeight: 300}, 4000, 3000, Geometry{400, 300, 400, 300}},
		{"inside no box", Profile{Fit: Inside}, 4000, 3000, Geometry{4000, 3000, 4000, 3000}},
		{"inside small no upscale", Profile{Fit: Inside, MaxWidth: 1000, MaxHeight: 1000}, 400, 300, Geometry{400, 300, 400, 300}},
		{"inside small upscale", Profile{Fit: Inside, MaxWidth: 1000, MaxHeight: 1000, Upscale: true}, 400, 300, Geometry{1000, 750, 1000, 750}},
		{"inside tiny side stays 1", Profile{Fit: Inside, MaxWidth: 10}, 10000, 10, Geometry{10, 1, 10, 1}},
		{"cover down", Profile{Fit: Cover, MaxWidth: 300, MaxHeight: 300}, 4000, 3000, Geometry{300, 300, 300, 300}},
		{"cover upscale", Profile{Fit: Cover, MaxWidth: 1000, MaxHeight: 500, Upscale: true}, 400, 300, Geometry{1000, 500, 1000, 500}},
		{"cover small keeps aspect, no upscale", Profile{Fit: Cover, MaxWidth: 1000, MaxHeight: 500}, 400, 300, Geometry{400, 200, 400, 200}},
		{"cover one side small", Profile{Fit: Cover, MaxWidth: 500, MaxHeight: 500}, 400, 3000, Geometry{400, 400, 400, 400}},
		{"contain down", Profile{Fit: Contain, MaxWidth: 300, MaxHeight: 300}, 4000, 3000, Geometry{300, 300, 300, 225}},
		{"contain small no upscale", Profile{Fit: Contain, MaxWidth: 1000, MaxHeight: 1000}, 400, 300, Geometry{1000, 1000, 400, 300}},
		{"contain small upscale", Profile{Fit: Contain, MaxWidth: 1000, MaxHeight: 1000, Upscale: true}, 400, 300, Geometry{1000, 1000, 1000, 750}},
		{"fill", Profile{Fit: Fill, MaxWidth: 300, MaxHeight: 100}, 4000, 3000, Geometry{300, 100, 300, 100}},
		{"fill small no upscale", Profile{Fit: Fill, MaxWidth: 1000, MaxHeight: 100}, 400, 300, Geometry{400, 100, 400, 100}},
		{"fill small upscale", Profile{Fit: Fill, MaxWidth: 1000, MaxHeight: 100, Upscale: true}, 400, 300, Geometry{1000, 100, 1000, 100}},
	}
	for _, c := range cases {
		if got := c.p.Geometry(c.w, c.h); got != c.want {
			t.Errorf("%s: %dx%d → %+v, want %+v", c.name, c.w, c.h, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Profile{Format: Webp, Fit: Inside, Quality: 80, MaxWidth: 100}
	if e := ok.Validate(); e != nil {
		t.Fatalf("valid profile refused: %v", e)
	}
	cases := []struct {
		name string
		p    Profile
		want code.Code
	}{
		{"no format", Profile{Fit: Inside, Quality: 80}, code.InvalidJob},
		{"unknown format", Profile{Format: "bmp", Fit: Inside, Quality: 80}, code.InvalidJob},
		{"format in capitals", Profile{Format: "WEBP", Fit: Inside, Quality: 80}, code.InvalidJob},
		{"input-only format", Profile{Format: Heic, Fit: Inside, Quality: 80}, code.InvalidJob},
		{"unknown fit", Profile{Format: Webp, Fit: "square", Quality: 80}, code.InvalidJob},
		{"jpg lossless", Profile{Format: Jpg, Fit: Inside, Quality: 80, Lossless: true}, code.InvalidJob},
		{"quality above 100", Profile{Format: Webp, Fit: Inside, Quality: 101}, code.InvalidQuality},
		{"negative quality", Profile{Format: Webp, Fit: Inside, Quality: -1}, code.InvalidQuality},
		{"lossy without quality", Profile{Format: Webp, Fit: Inside}, code.InvalidQuality},
		{"jpg without quality", Profile{Format: Jpg, Fit: Inside}, code.InvalidQuality},
		{"negative width", Profile{Format: Webp, Fit: Inside, Quality: 80, MaxWidth: -1}, code.InvalidDimensions},
		{"cover without height", Profile{Format: Webp, Fit: Cover, Quality: 80, MaxWidth: 100}, code.InvalidDimensions},
		{"contain without width", Profile{Format: Webp, Fit: Contain, Quality: 80, MaxHeight: 100}, code.InvalidDimensions},
		{"fill without box", Profile{Format: Webp, Fit: Fill, Quality: 80}, code.InvalidDimensions},
		{"negative blur", Profile{Format: Webp, Fit: Inside, Quality: 80, Blur: -1}, code.InvalidJob},
		{"nan blur", Profile{Format: Webp, Fit: Inside, Quality: 80, Blur: math.NaN()}, code.InvalidJob},
		{"blur too big", Profile{Format: Webp, Fit: Inside, Quality: 80, Blur: Profile{}.MaxBlur() + 0.01}, code.InvalidJob},
		{"negative sharpness", Profile{Format: Webp, Fit: Inside, Quality: 80, Sharpness: -0.1}, code.InvalidJob},
		{"sharpness too big", Profile{Format: Webp, Fit: Inside, Quality: 80, Sharpness: Profile{}.MaxSharpness() + 0.01}, code.InvalidJob},
		{"negative brightness", Profile{Format: Webp, Fit: Inside, Quality: 80, Brightness: -0.5}, code.InvalidJob},
		{"brightness too big", Profile{Format: Webp, Fit: Inside, Quality: 80, Brightness: Profile{}.MaxBrightness() + 0.01}, code.InvalidJob},
		{"inf brightness", Profile{Format: Webp, Fit: Inside, Quality: 80, Brightness: math.Inf(1)}, code.InvalidJob},
		{"negative max bytes", Profile{Format: Webp, Fit: Inside, Quality: 80, MaxBytes: -1}, code.InvalidJob},
		{"palette too big", Profile{Format: Webp, Fit: Inside, Quality: 80, PaletteColors: Profile{}.MaxPalette() + 1}, code.InvalidJob},
		{"bad colour", Profile{Format: Webp, Fit: Inside, Quality: 80, BackgroundColor: "red"}, code.InvalidJob},
	}
	for _, c := range cases {
		if e := c.p.Validate(); e == nil || e.Code != c.want {
			t.Errorf("%s: got %v, want %s", c.name, e, c.want)
		}
	}
	for _, p := range []Profile{
		{Format: Png, Fit: Inside},                                          // png needs no quality
		{Format: Avif, Fit: Inside, Lossless: true},                         // nor lossless
		{Format: Jpg, Fit: Cover, Quality: 1, MaxWidth: 1000, MaxHeight: 1}, // bounds inclusive
		{Format: Webp, Quality: 80},                                         // empty fit is inside
		{Format: Png, Fit: Fill, MaxWidth: 1, MaxHeight: 1, Blur: 3, Sharpness: 3, Brightness: 3},
	} {
		if e := p.Validate(); e != nil {
			t.Errorf("%+v refused: %v", p, e)
		}
	}
}

func TestBackground(t *testing.T) {
	cases := map[string]Color{
		"":          {},
		"#fff":      {255, 255, 255, 255},
		"#102030":   {16, 32, 48, 255},
		"#10203040": {16, 32, 48, 64},
		"#ABCDEF":   {171, 205, 239, 255},
	}
	for s, want := range cases {
		got, err := Profile{BackgroundColor: s}.Background()
		if err != nil || got != want {
			t.Errorf("%q → %+v, %v; want %+v", s, got, err, want)
		}
	}
	for _, s := range []string{"fff", "#ff", "#fffff", "#gggggg", "#1020304", "white", " #fff"} {
		if _, err := (Profile{BackgroundColor: s}).Background(); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

// TestParams checks the job processing parameters: empty is the bee
// default, otherwise within their limits
func TestParams(t *testing.T) {
	zero := 0
	nine := 9
	ten := 10

	good := []Profile{
		{Format: Avif, Quality: 60, Effort: &zero},
		{Format: Avif, Quality: 60, Effort: &nine, MinQuality: 20},
	}
	for _, p := range good {
		e := p.Validate()
		if e != nil {
			t.Errorf("%+v refused: %v", p, e)
		}
	}

	bad := map[string]Profile{
		"effort 10":       {Format: Avif, Quality: 60, Effort: &ten},
		"min quality 101": {Format: Avif, Quality: 60, MinQuality: 101},
		"min quality -1":  {Format: Avif, Quality: 60, MinQuality: -1},
	}
	for name, p := range bad {
		e := p.Validate()
		if e == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// TestJobValidate checks id, url, tasks and result keys
func TestJobValidate(t *testing.T) {
	profile := Profile{Format: Webp, Quality: 80, MaxWidth: 100}

	j := Job{
		ID:    "j1",
		Input: "https://example.com/a.jpg?sig=1",
		Tasks: []Task{
			{ID: "small", Output: "/img/a/small.webp", Profile: profile},
			{ID: "big", Output: "img/a/big.webp", Profile: profile},
		},
	}

	e := j.Validate()
	switch {
	case e != nil:
		t.Fatal(e)
	case j.Tasks[0].Output != "img/a/small.webp":
		t.Errorf("leading slash must be trimmed: %s", j.Tasks[0].Output)
	}

	cases := map[string]struct {
		edit func(j *Job)
		want code.Code
	}{
		"no id":          {func(j *Job) { j.ID = "" }, code.InvalidJob},
		"no input":       {func(j *Job) { j.Input = "" }, code.InvalidJob},
		"no tasks":       {func(j *Job) { j.Tasks = nil }, code.InvalidJob},
		"task no id":     {func(j *Job) { j.Tasks[1].ID = "" }, code.InvalidJob},
		"task id twice":  {func(j *Job) { j.Tasks[1].ID = "small" }, code.InvalidJob},
		"dirty output":   {func(j *Job) { j.Tasks[1].Output = "img/../x.webp" }, code.InvalidPath},
		"output twice":   {func(j *Job) { j.Tasks[1].Output = "img/a/small.webp" }, code.InvalidPath},
		"bad profile":    {func(j *Job) { j.Tasks[1].Profile.Format = "bmp" }, code.InvalidJob},
		"bad dimensions": {func(j *Job) { j.Tasks[1].Profile.MaxWidth = -1 }, code.InvalidDimensions},
	}
	for name, c := range cases {
		j := Job{
			ID:    "j1",
			Input: "https://example.com/a.jpg",
			Tasks: []Task{
				{ID: "small", Output: "img/a/small.webp", Profile: profile},
				{ID: "big", Output: "img/a/big.webp", Profile: profile},
			},
		}
		c.edit(&j)

		e := j.Validate()
		switch {
		case e == nil:
			t.Errorf("%s accepted", name)
		case e.Code != c.want:
			t.Errorf("%s: %s, want %s", name, e.Code, c.want)
		}
	}
}
