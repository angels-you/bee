package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/angels-you/bee/pkg/proto"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/angels-you/bee/internal/encrypt"
	videotool "github.com/angels-you/bee/internal/engine/video"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/video"
)

// videoRun is the state of one video job (Stream or Video)
type videoRun struct {
	*work

	// the job, already checked
	options    video.Options
	profiles   []video.Profile
	hls        *video.HLS // nil for Video
	thumbnails []video.Thumbnail
	preview    *video.Preview
	cipher     *encrypt.HLS // segment encryption; nil without a key

	// work
	source *videotool.File
	sound  *videotool.File
	mark   *videotool.File
	plan   *videotool.Plan
	report *videotool.Report
	res    video.Result

	// publishing
	renditions []*videoRendition
	entry      upload // entry: master, the only media playlist, or mp4
}

// videoRendition is one HLS variant as published: where its files go,
// its playlist, sizes and IVs of the published files
type videoRendition struct {
	profile  video.Profile
	output   videotool.Output
	dir      string // local folder
	folder   string // init and segments folder in storage
	playlist string // key of its media playlist
	text     []byte // media playlist as published (with keys)
	track    *track
}

// Stream runs one HLS job (see process)
func (a *Engine) Stream(ctx context.Context, j video.Stream) (video.Result, error) {
	r := &videoRun{work: a.newWork(proto.Stream, j.ID, j.Input)}

	// check a copy: the caller's job is not changed
	check := videoCheck{cfg: a.cfg}
	e := check.checkStream(&j)
	if e != nil {
		return r.fail(e)
	}

	r.output = j.Output
	r.options = j.Options
	r.profiles = j.Profiles
	r.hls = &j.HLS
	r.thumbnails = j.Thumbnails
	r.preview = j.Preview
	return r.process(ctx)
}

// Video runs one job with a single MP4 (see process)
func (a *Engine) Video(ctx context.Context, j video.Video) (video.Result, error) {
	r := &videoRun{work: a.newWork(proto.Video, j.ID, j.Input)}

	// check a copy: the caller's job is not changed
	check := videoCheck{cfg: a.cfg}
	e := check.checkVideo(&j)
	if e != nil {
		return r.fail(e)
	}

	r.output = j.Output
	r.options = j.Options
	r.profiles = []video.Profile{j.Profile}
	r.thumbnails = j.Thumbnails
	r.preview = j.Preview
	return r.process(ctx)
}

// process is the shared pipeline of Stream and Video: the ffmpeg build supports the job →
// slot → timeout → entry not yet published (else ALREADY_EXISTS or the
// saved result of this same job) → downloads → ffprobe and limits →
// plan → one ffmpeg run and check → upload in waves, entry
// last → result. The temp folder is removed in any case
func (a *videoRun) process(ctx context.Context) (video.Result, error) {
	e := a.supported()
	if e != nil {
		return a.fail(e)
	}

	// encryption
	if a.hls != nil && len(a.hls.Key) > 0 {
		cipher, err := encrypt.New(a.hls.Key)
		if err != nil {
			return a.fail(code.New(code.InvalidJob, err.Error(), nil))
		}
		a.cipher = cipher
	}

	// slot, timeout, folder
	ctx, end, e := a.begin(ctx, a.engine.cfg.Video.Timeout)
	if e != nil {
		return a.fail(e)
	}
	defer end()

	// entry already published: reject, or this job already ran
	stored, done, e := a.published(ctx)
	if e != nil {
		return a.fail(e)
	}
	if done {
		return a.again(stored)
	}

	// work
	e = a.download(ctx)
	if e != nil {
		return a.fail(e)
	}

	e = a.probe(ctx)
	if e != nil {
		return a.fail(e)
	}

	e = a.planOutputs()
	if e != nil {
		return a.fail(e)
	}

	e = a.encode(ctx)
	if e != nil {
		return a.fail(e)
	}

	e = a.upload(ctx)
	if e != nil {
		return a.fail(e)
	}

	return a.done()
}

// supported checks that the ffmpeg build can encode every codec and image format
// of the job (optional ones: HEVC, AV1, WebP, AVIF)
func (a *videoRun) supported() *code.Error {
	tools := a.engine.video

	for _, p := range a.profiles {
		if !tools.CanEncode(p.VideoCodec) {
			return code.New(code.UnsupportedCodec, "this bee has no "+string(p.VideoCodec)+" encoder", nil)
		}
	}

	if a.preview != nil && !tools.CanEncode(a.preview.Profile.VideoCodec) {
		return code.New(code.UnsupportedCodec, "this bee has no "+string(a.preview.Profile.VideoCodec)+" encoder for the preview", nil)
	}

	for _, t := range a.thumbnails {
		if !tools.CanWrite(t.Format) {
			return code.New(code.UnsupportedCodec, "this bee cannot write "+string(t.Format)+" thumbnails", nil)
		}
	}

	return nil
}

// download fetches the source, sound and watermark to the temp folder
func (a *videoRun) download(ctx context.Context) *code.Error {

	// source
	file, info, e := a.fetch(ctx, "input", a.input, "source")
	if e != nil {
		return e
	}
	a.source = &videotool.File{Path: file}
	a.res.SHA256 = info.SHA256
	a.res.SourceSize = info.Size

	// sound
	if a.options.Sound != "" {
		file, _, e = a.fetch(ctx, "options.sound", a.options.Sound, "sound")
		if e != nil {
			return e
		}
		a.sound = &videotool.File{Path: file}
	}

	// watermark
	if a.options.Watermark != nil {
		file, _, e = a.fetch(ctx, "options.watermark.input", a.options.Watermark.Input, "watermark")
		if e != nil {
			return e
		}
		a.mark = &videotool.File{Path: file}
	}

	return nil
}

// probe reads the downloaded files and checks them against limits: video
// of an accepted codec within duration and size, sound of an accepted
// codec, watermark is an image
func (a *videoRun) probe(ctx context.Context) *code.Error {
	e := a.probeSource(ctx)
	if e != nil {
		return e
	}

	e = a.probeSound(ctx)
	if e != nil {
		return e
	}

	return a.probeMark(ctx)
}

// probeSource probes the source
func (a *videoRun) probeSource(ctx context.Context) *code.Error {
	cfg := a.engine.cfg.Video

	m, err := a.engine.video.ProbeSource(ctx, a.source.Path)
	if err != nil {
		return a.probeError(ctx, code.InvalidVideo, "the source is not a readable video", err)
	}

	a.source.Media = m
	a.log = a.log.With("source_size", m.Size, "source_codec", m.VideoCodec, "source_width", m.DisplayWidth, "source_height", m.DisplayHeight, "source_duration", m.Duration)

	switch {
	case !m.HasVideo:
		return code.New(code.InvalidVideo, "the source has no video stream", nil)
	case !m.AcceptedSource():
		return code.New(code.UnsupportedFormat, "the source codec "+m.VideoCodec+" is not accepted", nil)
	case m.Duration < cfg.MinDuration.Seconds():
		return code.New(code.DurationLimitExceeded, fmt.Sprintf("the source lasts %.3fs, less than %s", m.Duration, cfg.MinDuration), nil)
	case m.Duration > cfg.MaxDuration.Seconds():
		return code.New(code.DurationLimitExceeded, fmt.Sprintf("the source lasts %.3fs, more than %s", m.Duration, cfg.MaxDuration), nil)
	case m.DisplayWidth > cfg.MaxDimension:
		return code.New(code.DimensionLimitExceeded, fmt.Sprintf("the source is %d px wide, more than %d", m.DisplayWidth, cfg.MaxDimension), nil)
	case m.DisplayHeight > cfg.MaxDimension:
		return code.New(code.DimensionLimitExceeded, fmt.Sprintf("the source is %d px high, more than %d", m.DisplayHeight, cfg.MaxDimension), nil)
	}

	return nil
}

// probeSound probes the external sound, if any
func (a *videoRun) probeSound(ctx context.Context) *code.Error {
	if a.sound == nil {
		return nil
	}

	m, err := a.engine.video.ProbeSound(ctx, a.sound.Path)
	if err != nil {
		return a.probeError(ctx, code.InvalidSound, "the sound is not readable audio", err)
	}

	a.sound.Media = m
	switch {
	case !m.HasAudio:
		return code.New(code.InvalidSound, "the sound has no audio stream", nil)
	case !m.AcceptedSound():
		return code.New(code.InvalidSound, "the sound codec "+m.AudioCodec+" is not accepted", nil)
	}

	return nil
}

// probeMark probes the watermark, if any
func (a *videoRun) probeMark(ctx context.Context) *code.Error {
	if a.mark == nil {
		return nil
	}

	limit := a.engine.cfg.Video.MaxDimension
	m, err := a.engine.video.ProbePicture(ctx, a.mark.Path)
	if err != nil {
		return a.probeError(ctx, code.InvalidWatermark, "the watermark is not a readable png, jpeg or webp", err)
	}

	a.mark.Media = m
	switch {
	case !m.AcceptedPicture():
		return code.New(code.InvalidWatermark, "the watermark is not a png, jpeg or webp picture", nil)
	case m.DisplayWidth > limit:
		return code.New(code.InvalidWatermark, fmt.Sprintf("the watermark is wider than %d px", limit), nil)
	case m.DisplayHeight > limit:
		return code.New(code.InvalidWatermark, fmt.Sprintf("the watermark is higher than %d px", limit), nil)
	}

	return nil
}

// probeError maps a file ffprobe could not read: an ended context
// as is, otherwise code c
func (a *videoRun) probeError(ctx context.Context, c code.Code, message string, err error) *code.Error {
	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	return code.New(c, message, err)
}

// planOutputs plans the ffmpeg run; the processed timeline also stays within
// the duration (Speed changes it)
func (a *videoRun) planOutputs() *code.Error {
	cfg := a.engine.cfg.Video
	in := videotool.Input{
		Source:     *a.source,
		Sound:      a.sound,
		Watermark:  a.mark,
		Options:    a.options,
		Profiles:   a.profiles,
		HLS:        a.hls,
		Thumbnails: a.thumbnails,
		Preview:    a.preview,
		LUFS:       a.options.LUFS,
		TruePeak:   a.options.TruePeak,
	}

	p, e := a.engine.video.Plan(in)
	if e != nil {
		return e
	}

	switch {
	case p.Duration < cfg.MinDuration.Seconds():
		return code.New(code.DurationLimitExceeded, fmt.Sprintf("the processed timeline lasts %.3fs, less than %s", p.Duration, cfg.MinDuration), nil)
	case p.Duration > cfg.MaxDuration.Seconds():
		return code.New(code.DurationLimitExceeded, fmt.Sprintf("the processed timeline lasts %.3fs, more than %s", p.Duration, cfg.MaxDuration), nil)
	}

	a.plan = p
	if len(p.Skipped) > 0 {
		a.log.Info("engine: renditions skipped, larger than the source", "skipped", p.Skipped)
	}

	return nil
}

// encode runs the loudness pass for Options.Normalize, one ffmpeg run,
// then reads and checks each result
func (a *videoRun) encode(ctx context.Context) *code.Error {
	t := time.Now()
	tools := a.engine.video

	// loudness
	err := tools.Normalize(ctx, a.plan)
	if err != nil {
		if ctx.Err() != nil {
			return code.As(ctx.Err())
		}
		return code.New(code.EncodeFailed, "loudness measure failed", err)
	}

	if a.plan.Loudness != nil {
		a.log.Info("engine: normalized", "lufs", a.plan.Loudness.Integrated, "true_peak", a.plan.Loudness.TruePeak, "gain_db", a.plan.Gain)
	}

	// ffmpeg
	err = tools.Encode(ctx, a.plan, a.dir)
	a.processing(t)
	if err != nil {
		if ctx.Err() != nil {
			return code.As(ctx.Err())
		}
		return code.New(code.EncodeFailed, "ffmpeg failed", err)
	}

	// check results
	report, e := tools.Verify(ctx, a.plan, a.dir)
	if e != nil {
		return e
	}

	a.report = report
	return nil
}

// upload sends everything to storage in waves: init and segments (encrypted
// with a key), preview images and previews; then the media playlists
// of multiple variants (with EXT-X-KEY lines, so after each
// segment and its IV); a check that everything is in place; result to the ledger;
// entry last, also checked. Entry published = everything published:
// a job that failed earlier leaves no entry and can be retried with the same
// keys
func (a *videoRun) upload(ctx context.Context) *code.Error {
	a.renditionsOf()

	// wave 1: media and extras
	e := a.putAll(ctx, a.mediaFiles())
	if e != nil {
		return e
	}

	// wave 2: media playlists with the IVs from wave 1
	lists, e := a.mediaPlaylists()
	if e != nil {
		return e
	}

	e = a.putAll(ctx, lists)
	if e != nil {
		return e
	}

	// entry and the result it completes
	a.entryOf()
	a.result()

	// everything except the entry is in place
	e = a.stored(ctx, a.required())
	if e != nil {
		return e
	}

	// the result is saved before the entry makes the job visible
	e = a.remember(ctx, a.res)
	if e != nil {
		return e
	}

	// entry last
	e = a.putAll(ctx, []upload{a.entry})
	if e != nil {
		return e
	}

	return a.stored(ctx, []string{a.output})
}

// renditionsOf builds the published view of each HLS variant: with several
// variants each goes to <output folder>/<name>/ with index.m3u8, a single
// variant sits next to output, which is its playlist
func (a *videoRun) renditionsOf() {
	if a.hls == nil {
		return
	}

	base := path.Dir(a.output)
	for i, r := range a.plan.Renditions {
		pub := &videoRendition{
			profile:  r.Profile,
			output:   a.report.Renditions[i],
			dir:      filepath.Join(a.dir, r.Dir),
			folder:   base,
			playlist: a.output,
			track:    newTrack(),
		}

		if len(a.profiles) > 1 {
			pub.folder = path.Join(base, r.Profile.Name)
			pub.playlist = path.Join(pub.folder, "index.m3u8")
		}

		a.renditions = append(a.renditions, pub)
	}
}

// mediaFiles lists wave 1: init and segments of each variant, preview images,
// previews
func (a *videoRun) mediaFiles() []upload {
	var list []upload

	for _, r := range a.renditions {
		pl := r.output.Playlist
		if pl.Init != "" {
			list = append(list, upload{key: path.Join(r.folder, pl.Init), file: filepath.Join(r.dir, pl.Init), track: r.track})
		}

		for _, s := range pl.Segments {
			list = append(list, upload{key: path.Join(r.folder, s.Name), file: filepath.Join(r.dir, s.Name), encrypt: a.cipher, track: r.track})
		}
	}

	for i, s := range a.plan.Stills {
		list = append(list, upload{key: a.thumbnails[i].Output, file: filepath.Join(a.dir, s.Path)})
	}

	if a.plan.Clip != nil {
		list = append(list, upload{key: a.preview.Output, file: filepath.Join(a.dir, a.plan.Clip.Path)})
	}

	return list
}

// mediaPlaylists builds each variant media playlist as published: with a
// key, an EXT-X-KEY line with its segment IV above each EXTINF. With
// several variants this is wave 2; the playlist of a single variant is the
// entry itself
func (a *videoRun) mediaPlaylists() ([]upload, *code.Error) {
	var list []upload

	for i, r := range a.renditions {
		text, err := os.ReadFile(filepath.Join(a.dir, a.plan.Renditions[i].File))
		if err != nil {
			return nil, code.New(code.InternalTemporaryError, "media playlist missing", err)
		}

		// key lines
		if a.cipher != nil {
			published, err := a.cipher.Playlist(string(text), a.hls.KeyURI, r.track.ivs)
			if err != nil {
				return nil, code.New(code.InternalTemporaryError, "media playlist keys", err)
			}
			text = []byte(published)
		}
		r.text = text

		if len(a.renditions) > 1 {
			list = append(list, upload{key: r.playlist, data: text})
		}
	}

	return list, nil
}

// entryOf builds the entry: master for several variants, media playlist for one, mp4
// for Video
func (a *videoRun) entryOf() {
	switch {
	case a.hls == nil:
		a.entry = upload{key: a.output, file: filepath.Join(a.dir, a.plan.Renditions[0].File)}
		return
	case len(a.renditions) == 1:
		a.entry = upload{key: a.output, data: a.renditions[0].text}
		return
	}

	// master: each variant measured from what was published
	master := videotool.Master{Version: a.renditions[0].output.Playlist.Version}
	for _, r := range a.renditions {
		peak, average := r.output.Playlist.Bandwidth(r.track.sizes)
		m := r.output.Media
		master.Variants = append(master.Variants, videotool.Variant{
			URI:              r.profile.Name + "/index.m3u8",
			Bandwidth:        peak,
			AverageBandwidth: average,
			Width:            m.Width,
			Height:           m.Height,
			FPS:              m.FPS,
			Codecs:           m.Codecs,
		})
	}

	a.entry = upload{key: a.output, data: []byte(master.String())}
}

// result builds the measured result: variants as published, previews,
// total size of all objects
func (a *videoRun) result() {
	res := video.Result{
		ID:         a.id,
		Output:     a.output,
		SHA256:     a.res.SHA256,
		SourceSize: a.res.SourceSize,
	}
	report := a.report

	// variants
	for i, out := range report.Renditions {
		pr := a.profileResult(a.plan.Renditions[i].Profile, out)

		if a.hls == nil {
			pr.Output = a.output
		} else {
			r := a.renditions[i]
			pr.Output = r.playlist
			pr.Size = int64(len(r.text))
			for _, size := range r.track.sizes {
				pr.Size += size
			}
			pr.Segments = len(out.Playlist.Segments)
		}

		res.Profiles = append(res.Profiles, pr)
		res.Size += pr.Size
	}
	res.Duration = report.Renditions[0].Media.Duration

	// master of several variants
	if a.hls != nil && len(a.renditions) > 1 {
		res.Size += int64(len(a.entry.data))
	}

	// extras
	for i, img := range report.Stills {
		t := a.thumbnails[i]
		res.Thumbnails = append(res.Thumbnails, video.ImageResult{Output: t.Output, Width: img.Width, Height: img.Height, Format: t.Format, Size: img.Size})
		res.Size += img.Size
	}

	if report.Preview != nil {
		pr := a.profileResult(a.preview.Profile, *report.Preview)
		pr.Output = a.preview.Output
		res.Preview = &pr
		res.Size += pr.Size
	}

	res.Elapsed = a.elapsed()
	a.res = res
}

// profileResult holds the measurements of one result; Size is the file (an HLS variant
// sets its own)
func (a *videoRun) profileResult(p video.Profile, out videotool.Output) video.ProfileResult {
	m := out.Media
	pr := video.ProfileResult{
		Name:         p.Name,
		Duration:     m.Duration,
		Width:        m.Width,
		Height:       m.Height,
		FPS:          m.FPS,
		VideoCodec:   p.VideoCodec,
		VideoBitrate: m.VideoBitrate,
		HasSound:     m.HasAudio,
		Size:         out.Size,
	}

	if m.HasAudio {
		pr.SoundCodec = p.SoundCodec
		pr.SoundBitrate = m.AudioBitrate
	}

	return pr
}

// required lists the objects checked before the entry: for each variant the media
// playlist (if there are several variants), init, first and last segment;
// preview images; previews
func (a *videoRun) required() []string {
	var keys []string

	for _, r := range a.renditions {
		pl := r.output.Playlist
		if len(a.renditions) > 1 {
			keys = append(keys, r.playlist)
		}

		if pl.Init != "" {
			keys = append(keys, path.Join(r.folder, pl.Init))
		}

		keys = append(keys, path.Join(r.folder, pl.Segments[0].Name))
		keys = append(keys, path.Join(r.folder, pl.Segments[len(pl.Segments)-1].Name))
	}

	for _, t := range a.thumbnails {
		keys = append(keys, t.Output)
	}

	if a.preview != nil {
		keys = append(keys, a.preview.Output)
	}

	return keys
}

// done returns a successful result
func (a *videoRun) done() (video.Result, error) {
	a.res.Elapsed = a.elapsed()
	a.work.done("renditions", len(a.res.Profiles), "duration", a.res.Duration)

	return a.res, nil
}

// again returns the saved result of a job that already ran
func (a *videoRun) again(stored []byte) (video.Result, error) {
	var res video.Result
	err := json.Unmarshal(stored, &res)
	if err != nil {
		return a.fail(code.New(code.InternalTemporaryError, "ledger result is not json", err))
	}

	a.work.again()
	return res, nil
}

// fail returns a failed result: id and error, nothing else is computed
func (a *videoRun) fail(e *code.Error) (video.Result, error) {
	a.res = video.Result{ID: a.id, Elapsed: a.elapsed(), Error: e}
	a.work.fail(e)

	return a.res, e
}
