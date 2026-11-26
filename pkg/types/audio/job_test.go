package audio

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/angels-you/bee/pkg/types/code"
)

// fixture — правильная job, которую тесты портят по одному полю
type fixture struct{}

// job — правильная job со всеми ключами
func (a fixture) job() Job {
	return Job{
		ID:       "t1",
		Input:    "https://example.com/a.wav?X-Amz-Signature=1",
		Output:   "/tracks/1/master.m3u8",
		Wave:     "tracks/1/wave.json",
		Meta:     "tracks/1/meta.json",
		Analysis: "tracks/1/analysis.json",
	}
}

// TestValidate — правильная job проходит, ключи чистятся
func TestValidate(t *testing.T) {
	f := fixture{}
	j := f.job()

	e := j.Validate()
	switch {
	case e != nil:
		t.Fatal(e)
	case j.Output != "tracks/1/master.m3u8":
		t.Errorf("leading slash must be trimmed: %s", j.Output)
	}

	// только обязательное
	j = Job{ID: "t2", Input: "https://example.com/a.wav", Output: "a.m3u8"}
	e = j.Validate()
	if e != nil {
		t.Errorf("minimal job refused: %v", e)
	}

	// с ключом шифрования
	j = f.job()
	j.Key = []byte("0123456789abcdef")
	j.KeyURI = "skd://1"
	e = j.Validate()
	if e != nil {
		t.Errorf("job with a key refused: %v", e)
	}
}

// TestRefuse — каждое неверное поле отклоняется своим кодом
func TestRefuse(t *testing.T) {
	f := fixture{}

	cases := map[string]struct {
		edit func(j *Job)
		want code.Code
	}{
		"no id":          {func(j *Job) { j.ID = "" }, code.InvalidJob},
		"no input":       {func(j *Job) { j.Input = "" }, code.InvalidJob},
		"input no host":  {func(j *Job) { j.Input = "a.wav" }, code.InvalidJob},
		"no output":      {func(j *Job) { j.Output = "" }, code.InvalidPath},
		"output not hls": {func(j *Job) { j.Output = "a/master.mp4" }, code.InvalidPath},
		"dirty output":   {func(j *Job) { j.Output = "a/../master.m3u8" }, code.InvalidPath},
		"dirty wave":     {func(j *Job) { j.Wave = "a//wave.json" }, code.InvalidPath},
		"same keys":      {func(j *Job) { j.Meta = j.Wave }, code.InvalidPath},
		"short key":      {func(j *Job) { j.Key = []byte("short"); j.KeyURI = "skd://1" }, code.InvalidJob},
		"key no uri":     {func(j *Job) { j.Key = []byte("0123456789abcdef") }, code.InvalidJob},
		"key bad uri":    {func(j *Job) { j.Key = []byte("0123456789abcdef"); j.KeyURI = "skd://\"1" }, code.InvalidJob},
		"lufs positive":  {func(j *Job) { j.Options.LUFS = 1 }, code.InvalidJob},
		"lufs too low":   {func(j *Job) { j.Options.LUFS = -70 }, code.InvalidJob},
		"peak positive":  {func(j *Job) { j.Options.TruePeak = 0.5 }, code.InvalidJob},
		"rendition zero": {func(j *Job) { j.Options.Renditions = []int{128, 0} }, code.InvalidJob},
		"rendition dup":  {func(j *Job) { j.Options.Renditions = []int{128, 128} }, code.InvalidJob},
		"negative rate":  {func(j *Job) { j.Options.SampleRate = -1 }, code.InvalidJob},
		"negative seg":   {func(j *Job) { j.Options.Segment = -1 }, code.InvalidJob},
		"negative pps":   {func(j *Job) { j.Options.WaveformPPS = -1 }, code.InvalidJob},
		"negative pts":   {func(j *Job) { j.Options.WaveformPoints = -1 }, code.InvalidJob},
	}

	for name, c := range cases {
		j := f.job()
		c.edit(&j)

		e := j.Validate()
		switch {
		case e == nil:
			t.Errorf("%s accepted", name)
		case e.Code != c.want:
			t.Errorf("%s: %s, want %s", name, e.Code, c.want)
		case strings.Contains(e.Error(), "X-Amz-Signature"):
			t.Errorf("%s: url in the error", name)
		}
	}
}

// TestParams — параметры в job: пустые не попадают в json и означают
// дефолт bee, false у флага отличается от nil
func TestParams(t *testing.T) {
	js, err := json.Marshal(Options{})
	if err != nil {
		t.Fatal(err)
	}

	if string(js) != `{}` {
		t.Errorf("empty options in json: %s", js)
	}

	var j Job
	raw := `{"id":"t1","options":{"renditions":[96,192],"lufs":-16,"sample_rate":48000,"analysis":false}}`
	err = json.Unmarshal([]byte(raw), &j)
	switch {
	case err != nil:
		t.Fatal(err)
	case len(j.Options.Renditions) != 2:
		t.Errorf("renditions %v", j.Options.Renditions)
	case j.Options.Analysis == nil:
		t.Error("analysis false must be set")
	case *j.Options.Analysis:
		t.Error("analysis must be false")
	case j.Options.Fingerprint != nil:
		t.Error("fingerprint must stay nil")
	}
}

// TestResultJSON — у упавшей job ошибка без причины
func TestResultJSON(t *testing.T) {
	failed := Result{ID: "t1", Error: code.New(code.InvalidAudio, "silent", nil)}

	js, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"id":"t1","elapsed":0,"encrypted":false,"error":{"code":"INVALID_AUDIO","message":"silent","retryable":false}}`
	if string(js) != want {
		t.Errorf("failed result %s", js)
	}
}
