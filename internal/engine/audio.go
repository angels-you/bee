package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/angels-you/bee/pkg/proto"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/angels-you/bee/internal/encrypt"
	audiotool "github.com/angels-you/bee/internal/engine/audio"
	"github.com/angels-you/bee/pkg/types/audio"
	"github.com/angels-you/bee/pkg/types/code"
)

// audioRun is the state of one audio job
type audioRun struct {
	*work
	job audio.Job
	res audio.Result
	hls *encrypt.HLS // segment encryption; nil without a key

	source     string // downloaded source
	meta       audio.Metadata
	analysis   audiotool.Analysis
	gain       float64
	renditions []audioRendition // in Options.Renditions order
	files      []upload         // init, segments and waveform json
}

// audioRendition is one HLS bitrate: ffmpeg files and what was published from them
type audioRendition struct {
	kbit     int
	folder   string // folder name: bitrate in kbit/s
	dir      string // local, as written by ffmpeg
	key      string // folder in storage: <master folder>/<folder>
	playlist audiotool.Playlist
	track    *track
}

// Audio runs one audio job: check and defaults → slot → download → ffprobe and limits → analysis → fingerprint →
// loudness → HLS of all variants → waveform → upload in waves, master
// last → verify. A failed job returns a Result with Error and the same
// *code.Error as the error
func (a *Engine) Audio(ctx context.Context, j audio.Job) (audio.Result, error) {
	r := &audioRun{work: a.newWork(proto.Audio, j.ID, j.Input), job: j}
	r.res.ID = j.ID
	r.res.Metrics = map[string]int64{}

	// check a copy: the caller's job is not changed
	r.job.Options.Renditions = slices.Clone(j.Options.Renditions)
	e := a.checkAudio(&r.job)
	if e != nil {
		return r.fail(e)
	}
	r.output = r.job.Output
	r.log = r.log.With("output", r.job.Output)

	// encryption
	if len(r.job.Key) > 0 {
		hls, err := encrypt.New(r.job.Key)
		if err != nil {
			return r.fail(code.New(code.InvalidJob, err.Error(), nil))
		}
		r.hls = hls
	}

	// slot, timeout, folder
	ctx, end, e := r.begin(ctx, a.cfg.Audio.Timeout)
	if e != nil {
		return r.fail(e)
	}
	defer end()

	// work
	e = r.pipeline(ctx)
	if e != nil {
		return r.fail(e)
	}

	return r.done()
}

// checkAudio checks the job without settings (the type's Validate), fills empty
// parameters with config defaults, then what depends on them: variant folders
func (a *Engine) checkAudio(j *audio.Job) *code.Error {
	e := j.Validate()
	if e != nil {
		return e
	}

	// bee defaults
	cfg := a.cfg.Audio
	o := &j.Options
	if o.LUFS == 0 {
		o.LUFS = cfg.LUFS
	}
	if o.TruePeak == 0 {
		o.TruePeak = cfg.TruePeak
	}
	if len(o.Renditions) == 0 {
		o.Renditions = slices.Clone(cfg.Renditions)
	}
	if o.SampleRate == 0 {
		o.SampleRate = cfg.SampleRate
	}
	if o.Channels == 0 {
		o.Channels = cfg.Channels
	}
	if o.Segment == 0 {
		o.Segment = cfg.Segment
	}
	if o.WaveformPPS == 0 {
		o.WaveformPPS = cfg.WaveformPPS
	}
	if o.WaveformPoints == 0 {
		o.WaveformPoints = cfg.WaveformPoints
	}
	if o.Analysis == nil {
		analysis := cfg.Analysis
		o.Analysis = &analysis
	}
	if o.Fingerprint == nil {
		fingerprint := cfg.Fingerprint
		o.Fingerprint = &fingerprint
	}

	// each variant folder next to master belongs to HLS
	others := map[string]string{"wave": j.Wave, "meta": j.Meta, "analysis": j.Analysis}
	for _, kbit := range o.Renditions {
		folder := path.Join(path.Dir(j.Output), strconv.Itoa(kbit)) + "/"

		for name, key := range others {
			if strings.HasPrefix(key, folder) {
				return code.New(code.InvalidPath, name+" lies in the folder of the "+strconv.Itoa(kbit)+" kbit/s rendition", nil)
			}
		}
	}

	return nil
}

// pipeline runs the stages in order, timing each in Result.Metrics
func (a *audioRun) pipeline(ctx context.Context) *code.Error {
	e := a.stage(ctx, "download", a.download)
	if e != nil {
		return e
	}

	e = a.stage(ctx, "probe", a.probe)
	if e != nil {
		return e
	}

	e = a.stage(ctx, "analysis", a.analyze)
	if e != nil {
		return e
	}

	e = a.stage(ctx, "fingerprint", a.fingerprint)
	if e != nil {
		return e
	}

	e = a.stage(ctx, "normalize", a.normalize)
	if e != nil {
		return e
	}

	e = a.stage(ctx, "encode", a.encode)
	if e != nil {
		return e
	}

	e = a.stage(ctx, "waveform", a.waveform)
	if e != nil {
		return e
	}

	e = a.stage(ctx, "upload", a.upload)
	if e != nil {
		return e
	}

	return a.stage(ctx, "verify", a.verify)
}

// stage runs one stage; its time goes to Result.Metrics under name
func (a *audioRun) stage(ctx context.Context, name string, fn func(context.Context) *code.Error) *code.Error {
	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	t := time.Now()
	e := fn(ctx)
	a.res.Metrics[name] = time.Since(t).Milliseconds()

	if e != nil {
		return e
	}

	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	return nil
}

// warn adds a non-critical quality note
func (a *audioRun) warn(c, message string) {
	a.res.Warnings = append(a.res.Warnings, audio.Warning{Code: c, Message: message})
}

// failed maps a tool error: an ended context as is, otherwise code c
func (a *audioRun) failed(ctx context.Context, c code.Code, message string, err error) *code.Error {
	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	return code.New(c, message+": "+a.short(err), err)
}

// download fetches the source to <dir>/source
func (a *audioRun) download(ctx context.Context) *code.Error {
	file, info, e := a.fetch(ctx, "input", a.job.Input, "source")
	if e != nil {
		return e
	}

	a.source = file
	a.res.SHA256 = info.SHA256
	a.res.SourceSize = info.Size
	return nil
}

// probe runs ffprobe, checks the format and limits
func (a *audioRun) probe(ctx context.Context) *code.Error {
	cfg := a.engine.cfg.Audio
	tools := a.engine.audio

	// ffprobe
	m, err := tools.Probe(ctx, a.source)
	if err != nil {
		return a.failed(ctx, code.InvalidAudio, "audio cannot be decoded", err)
	}

	m.SHA256 = a.res.SHA256
	if m.Size == 0 {
		m.Size = a.res.SourceSize
	}

	// format and duration
	dur := time.Duration(m.DurationMs) * time.Millisecond
	switch {
	case !tools.Supported(&m):
		return code.New(code.UnsupportedFormat, fmt.Sprintf("codec %s is not supported", m.Codec), nil)
	case dur > cfg.MaxDuration:
		return code.New(code.DurationLimitExceeded, fmt.Sprintf("duration %s is longer than %s", dur.Round(time.Second), cfg.MaxDuration), nil)
	case dur < cfg.MinDuration:
		return code.New(code.DurationLimitExceeded, fmt.Sprintf("duration %s is shorter than %s", dur, cfg.MinDuration), nil)
	}

	a.meta = m
	a.res.Meta = &a.meta

	// quality notes about the source
	lossy := false
	switch m.Codec {
	case "mp3", "aac", "vorbis", "opus":
		lossy = true
	}

	if lossy && m.Bitrate > 0 && m.Bitrate < 128000 {
		a.warn("low_bitrate", fmt.Sprintf("source bitrate %d kbps", m.Bitrate/1000))
	}

	if m.SampleRate < 44100 {
		a.warn("low_sample_rate", fmt.Sprintf("source sample rate %d Hz", m.SampleRate))
	}

	return nil
}

// analyze makes one decode pass: loudness, quality, pcm for the waveform
func (a *audioRun) analyze(ctx context.Context) *code.Error {
	t := time.Now()
	defer a.took(&a.processMs, t)

	cfg := a.engine.cfg.Audio
	o := audiotool.AnalyzeOptions{
		Quality:     *a.job.Options.Analysis,
		SilenceDB:   cfg.SilenceDB,
		SilenceMin:  cfg.SilenceSec,
		DurationSec: float64(a.meta.DurationMs) / 1000,
	}
	if a.job.Wave != "" {
		o.PCMPath = filepath.Join(a.dir, "pcm.raw")
	}

	// analysis
	res, err := a.engine.audio.Analyze(ctx, a.source, o)
	if err != nil {
		return a.failed(ctx, code.InvalidAudio, "audio analysis failed", err)
	}

	// no sample above the floor: digital silence for the whole track, or no
	// samples at all: nothing to encode and publish
	if res.Loudness.SamplePeak <= a.engine.audio.LevelFloor() {
		return code.New(code.InvalidAudio, "track is silent", nil)
	}

	a.analysis = res
	a.res.Loudness = &a.analysis.Loudness

	// loudness
	l := res.Loudness
	switch {
	case l.Integrated < -60:
		a.warn("silent", "track is silent or nearly silent")
	case l.Integrated < -30:
		a.warn("too_quiet", fmt.Sprintf("integrated loudness %.1f LUFS", l.Integrated))
	}

	if !o.Quality {
		return nil
	}

	// quality
	a.res.Quality = &a.analysis.Quality
	a.qualityWarnings(o.DurationSec)
	return nil
}

// qualityWarnings builds notes from the quality diagnostics
func (a *audioRun) qualityWarnings(duration float64) {
	q := a.analysis.Quality

	if q.Clipping {
		a.warn("clipping", fmt.Sprintf("digital clipping: flat factor %.1f dB at full scale", q.FlatFactor))
	}

	if q.LeadingSilence >= 5 {
		a.warn("leading_silence", fmt.Sprintf("%.1fs of silence at the start", q.LeadingSilence))
	}

	if q.TrailingSilence >= 5 {
		a.warn("trailing_silence", fmt.Sprintf("%.1fs of silence at the end", q.TrailingSilence))
	}

	// long silence inside the track
	for _, s := range q.Silences {
		switch {
		case s.Start <= 0.05:
		case s.End >= duration-0.05:
		case s.Duration < 10:
		default:
			a.warn("long_silence", fmt.Sprintf("%.1fs of silence at %.1fs", s.Duration, s.Start))
			return
		}
	}

	if q.ChannelImbalance >= 6 {
		a.warn("channel_imbalance", fmt.Sprintf("channels differ by %.1f dB", q.ChannelImbalance))
	}

	if math.Abs(q.DCOffset) >= 0.05 {
		a.warn("dc_offset", fmt.Sprintf("dc offset %.3f", q.DCOffset))
	}
}

// fingerprint computes chromaprint on request; an error is only a note
func (a *audioRun) fingerprint(ctx context.Context) *code.Error {
	if !*a.job.Options.Fingerprint {
		return nil
	}

	fp, err := a.engine.audio.Fingerprint(ctx, a.source, 120)
	if err != nil {
		if ctx.Err() != nil {
			return code.As(ctx.Err())
		}

		a.warn("fingerprint", "fingerprint failed: "+a.short(err))
		return nil
	}

	a.res.Fingerprint = &fp
	return nil
}

// normalize applies linear gain only
func (a *audioRun) normalize(ctx context.Context) *code.Error {
	o := a.job.Options
	if !o.Normalize {
		return nil
	}

	gain, limited := a.engine.audio.ComputeGain(a.analysis.Loudness, o.LUFS, o.TruePeak)
	a.gain = gain
	a.analysis.Loudness.Gain = gain
	a.analysis.Loudness.GainLimited = limited

	if limited {
		a.warn("gain_limited", fmt.Sprintf("gain reduced to %.2f dB to keep true peak under %.1f dBTP", gain, o.TruePeak))
	}

	return nil
}

// encode encodes all variants in one ffmpeg run, then checks and aligns the media
// playlist of each, and measures the result loudness
func (a *audioRun) encode(ctx context.Context) *code.Error {
	t := time.Now()
	defer a.processing(t)

	o := a.job.Options
	tools := a.engine.audio
	dir := filepath.Join(a.dir, "hls")

	// encoding
	err := tools.EncodeHLS(ctx, a.source, dir, audiotool.EncodeOptions{
		GainDB:         a.gain,
		Bitrates:       o.Renditions,
		SampleRate:     o.SampleRate,
		Channels:       o.Channels,
		SegmentSeconds: o.Segment,
	})
	if err != nil {
		return a.failed(ctx, code.EncodeFailed, "encoding failed", err)
	}

	// the player switches variants at segment boundaries: each variant
	// must be cut exactly like the first
	for _, kbit := range o.Renditions {
		e := a.rendition(ctx, dir, kbit)
		if e != nil {
			return e
		}
	}

	// result loudness
	a.measure(ctx)

	// init and segments of each variant
	for i := range a.renditions {
		r := &a.renditions[i]
		a.files = append(a.files, upload{key: path.Join(r.key, r.playlist.Init), file: filepath.Join(r.dir, r.playlist.Init), track: r.track})

		for _, s := range r.playlist.Segments {
			a.files = append(a.files, upload{key: path.Join(r.key, s.Name), file: filepath.Join(r.dir, s.Name), track: r.track, encrypt: a.hls})
		}
	}

	return nil
}

// rendition checks that the media playlist of variant kbit matches the first
func (a *audioRun) rendition(ctx context.Context, dir string, kbit int) *code.Error {
	folder := strconv.Itoa(kbit)

	pl, err := a.engine.audio.VerifyHLS(ctx, filepath.Join(dir, folder), float64(a.meta.DurationMs)/1000)
	if err != nil {
		return a.failed(ctx, code.VerifyFailed, "hls verification failed: "+folder, err)
	}

	if len(a.renditions) > 0 && !slices.Equal(pl.Segments, a.renditions[0].playlist.Segments) {
		return code.New(code.VerifyFailed, fmt.Sprintf("hls verification failed: segments of %s differ from %s", folder, a.renditions[0].folder), nil)
	}

	a.renditions = append(a.renditions, audioRendition{
		kbit:     kbit,
		folder:   folder,
		dir:      filepath.Join(dir, folder),
		key:      path.Join(path.Dir(a.job.Output), folder),
		playlist: pl,
		track:    newTrack(),
	})
	return nil
}

// measure takes the loudness of what the listener hears first: the first variant,
// the player starts with it; the rest are the same signal at another bitrate
func (a *audioRun) measure(ctx context.Context) {
	first := filepath.Join(a.renditions[0].dir, a.engine.audio.PlaylistName())

	out, err := a.engine.audio.Analyze(ctx, first, audiotool.AnalyzeOptions{})
	if err != nil {
		a.warn("output_measure", "output loudness could not be measured: "+a.short(err))
		return
	}

	a.analysis.Loudness.OutputLUFS = out.Loudness.Integrated
	a.analysis.Loudness.OutputTruePeak = out.Loudness.TruePeak
	a.analysis.Loudness.OutputLRA = out.Loudness.LRA

	peak := a.job.Options.TruePeak
	if a.job.Options.Normalize && out.Loudness.TruePeak > peak+0.5 {
		a.warn("true_peak", fmt.Sprintf("output true peak %.2f dBTP above %.1f", out.Loudness.TruePeak, peak))
	}
}

// waveform writes json to Job.Wave and Result.Waveform (its peaks) from the analysis pcm
func (a *audioRun) waveform(ctx context.Context) *code.Error {
	if a.job.Wave == "" {
		return nil
	}

	o := a.job.Options
	pcm := filepath.Join(a.dir, "pcm.raw")

	// pcm from the analysis pass
	f, err := os.Open(pcm)
	if err != nil {
		return code.New(code.InternalTemporaryError, "pcm missing", err)
	}

	w, err := a.engine.audio.BuildWaveform(f, a.meta.SampleRate, o.WaveformPPS, a.gain)
	f.Close()
	os.Remove(pcm)
	if err != nil {
		return code.New(code.InvalidAudio, "waveform: "+a.short(err), err)
	}

	// json and peaks
	js, err := json.Marshal(w)
	if err != nil {
		return code.New(code.InternalTemporaryError, "waveform json", err)
	}

	a.files = append(a.files, upload{key: a.job.Wave, data: js})
	a.res.Waveform = w.Peaks(o.WaveformPoints)
	return nil
}

// upload publishes in waves, master last: init, segments and waveform json;
// metadata and analysis json (metadata lists the variants, measured
// from the published segments); media playlists;
// master. A published master is a complete set: on any error it is not
// published. With a key, segments are encrypted and media playlists get
// key lines; master has no key
func (a *audioRun) upload(ctx context.Context) *code.Error {
	a.res.Encrypted = a.hls != nil

	// wave 1: files
	e := a.putAll(ctx, a.files)
	if e != nil {
		return e
	}

	// media playlists and result variants
	lists, variants, e := a.playlists()
	if e != nil {
		return e
	}

	// wave 2: json
	side, e := a.sideFiles()
	if e != nil {
		return e
	}

	e = a.putAll(ctx, side)
	if e != nil {
		return e
	}

	// wave 3: media playlists
	e = a.putAll(ctx, lists)
	if e != nil {
		return e
	}

	// master last: whoever sees it sees the complete set
	master := upload{key: a.job.Output, data: []byte(a.engine.audio.Master(variants))}
	e = a.putAll(ctx, []upload{master})
	if e != nil {
		return e
	}

	a.res.Size = a.uploaded
	return nil
}

// playlists builds each variant media playlist as published (with keys)
// and the variants of master and of the result
func (a *audioRun) playlists() ([]upload, []audiotool.Variant, *code.Error) {
	lists := make([]upload, 0, len(a.renditions))
	variants := make([]audiotool.Variant, 0, len(a.renditions))

	for i := range a.renditions {
		r := &a.renditions[i]

		// ffmpeg text
		text, err := os.ReadFile(filepath.Join(r.dir, a.engine.audio.PlaylistName()))
		if err != nil {
			return nil, nil, code.New(code.InternalTemporaryError, "playlist missing", err)
		}

		// key lines
		if a.hls != nil {
			published, err := a.hls.Playlist(string(text), a.job.KeyURI, r.track.ivs)
			if err != nil {
				return nil, nil, code.New(code.InternalTemporaryError, "playlist encryption: "+a.short(err), err)
			}
			text = []byte(published)
		}

		// measured from what was published
		peak, average := r.playlist.Bandwidth(r.track.sizes)
		size := int64(len(text))
		for _, n := range r.track.sizes {
			size += n
		}

		a.res.Renditions = append(a.res.Renditions, audio.Rendition{
			Folder:           r.folder,
			Bitrate:          r.kbit * 1000,
			AverageBandwidth: average,
			PeakBandwidth:    peak,
			Size:             size,
		})
		variants = append(variants, audiotool.Variant{
			URI:              r.folder + "/" + a.engine.audio.PlaylistName(),
			Bandwidth:        peak,
			AverageBandwidth: average,
		})
		lists = append(lists, upload{key: path.Join(r.key, a.engine.audio.PlaylistName()), data: text})
	}

	return lists, variants, nil
}

// sideFiles builds the metadata and analysis json, if the job asks for them
func (a *audioRun) sideFiles() ([]upload, *code.Error) {
	var side []upload

	// metadata
	if a.job.Meta != "" {
		meta := struct {
			SHA256     string            `json:"sha256"`
			Renditions []audio.Rendition `json:"renditions"`
			Meta       *audio.Metadata   `json:"meta"`
		}{a.res.SHA256, a.res.Renditions, &a.meta}

		data, err := json.Marshal(meta)
		if err != nil {
			return nil, code.New(code.InternalTemporaryError, "metadata json", err)
		}
		side = append(side, upload{key: a.job.Meta, data: data})
	}

	// analysis: only what is safe to publish
	if a.job.Analysis != "" {
		analysis := struct {
			Loudness *audio.Loudness `json:"loudness,omitempty"`
			Quality  *audio.Quality  `json:"quality,omitempty"`
			Warnings []audio.Warning `json:"warnings,omitempty"`
		}{a.res.Loudness, a.res.Quality, a.res.Warnings}

		data, err := json.Marshal(analysis)
		if err != nil {
			return nil, code.New(code.InternalTemporaryError, "analysis json", err)
		}
		side = append(side, upload{key: a.job.Analysis, data: data})
	}

	return side, nil
}

// verify checks that the needed objects are really there: master, for each
// variant the media playlist, init, first and last segment, and every
// json the job asked for
func (a *audioRun) verify(ctx context.Context) *code.Error {
	required := []string{a.job.Output}

	for _, r := range a.renditions {
		segments := r.playlist.Segments
		required = append(required,
			path.Join(r.key, a.engine.audio.PlaylistName()),
			path.Join(r.key, r.playlist.Init),
			path.Join(r.key, segments[0].Name),
			path.Join(r.key, segments[len(segments)-1].Name),
		)
	}

	for _, key := range []string{a.job.Wave, a.job.Meta, a.job.Analysis} {
		if key != "" {
			required = append(required, key)
		}
	}

	return a.stored(ctx, required)
}

// short trims an error to one line for messages, at most 300 bytes
func (a *audioRun) short(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	if len(s) > 300 {
		s = s[:300]
	}

	return s
}

// done returns a successful result
func (a *audioRun) done() (audio.Result, error) {
	a.res.Elapsed = a.elapsed()
	a.res.Metrics["total"] = a.res.Elapsed
	a.work.done("renditions", len(a.res.Renditions))

	return a.res, nil
}

// fail returns a failed result: id, error and what is known about the source
func (a *audioRun) fail(e *code.Error) (audio.Result, error) {
	a.res.Error = e
	a.res.Elapsed = a.elapsed()
	a.res.Metrics["total"] = a.res.Elapsed
	a.res.Renditions = nil
	a.res.Waveform = nil
	a.res.Size = 0
	a.work.fail(e)

	return a.res, e
}
