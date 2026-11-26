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

// videoRun — состояние одной видео job (Stream или Video)
type videoRun struct {
	*work

	// job, уже проверенная
	options    video.Options
	profiles   []video.Profile
	hls        *video.HLS // nil у Video
	thumbnails []video.Thumbnail
	preview    *video.Preview
	cipher     *encrypt.HLS // шифрование сегментов; nil без ключа

	// работа
	source *videotool.File
	sound  *videotool.File
	mark   *videotool.File
	plan   *videotool.Plan
	report *videotool.Report
	res    video.Result

	// публикация
	renditions []*videoRendition
	entry      upload // вход: master, единственный media playlist или mp4
}

// videoRendition — один вариант HLS как публикуется: куда идут его файлы,
// его playlist, размеры и IV опубликованных файлов
type videoRendition struct {
	profile  video.Profile
	output   videotool.Output
	dir      string // локальная папка
	folder   string // папка init и сегментов в хранилище
	playlist string // ключ его media playlist
	text     []byte // media playlist как опубликован (с ключами)
	track    *track
}

// Stream — одна HLS job (см. process)
func (a *Engine) Stream(ctx context.Context, j video.Stream) (video.Result, error) {
	r := &videoRun{work: a.newWork(proto.Stream, j.ID, j.Input)}

	// проверка на копии: job вызывающего не меняется
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

// Video — одна job с одним MP4 (см. process)
func (a *Engine) Video(ctx context.Context, j video.Video) (video.Result, error) {
	r := &videoRun{work: a.newWork(proto.Video, j.ID, j.Input)}

	// проверка на копии: job вызывающего не меняется
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

// process — общий пайплайн Stream и Video: сборка ffmpeg умеет job →
// слот → таймаут → вход ещё не опубликован (иначе ALREADY_EXISTS или
// сохранённый результат этой же job) → скачивания → ffprobe и лимиты →
// план → один запуск ffmpeg и проверка → загрузка волнами, вход
// последним → результат. Временная папка удаляется в любом случае
func (a *videoRun) process(ctx context.Context) (video.Result, error) {
	e := a.supported()
	if e != nil {
		return a.fail(e)
	}

	// шифрование
	if a.hls != nil && len(a.hls.Key) > 0 {
		cipher, err := encrypt.New(a.hls.Key)
		if err != nil {
			return a.fail(code.New(code.InvalidJob, err.Error(), nil))
		}
		a.cipher = cipher
	}

	// слот, таймаут, папка
	ctx, end, e := a.begin(ctx, a.engine.cfg.Video.Timeout)
	if e != nil {
		return a.fail(e)
	}
	defer end()

	// вход уже опубликован: отказ или эта job уже выполнена
	stored, done, e := a.published(ctx)
	if e != nil {
		return a.fail(e)
	}
	if done {
		return a.again(stored)
	}

	// работа
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

// supported — каждый кодек и формат картинки job сборка ffmpeg умеет
// кодировать (необязательные: HEVC, AV1, WebP, AVIF)
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

// download — оригинал, звук и водяной знак во временную папку
func (a *videoRun) download(ctx context.Context) *code.Error {

	// оригинал
	file, info, e := a.fetch(ctx, "input", a.input, "source")
	if e != nil {
		return e
	}
	a.source = &videotool.File{Path: file}
	a.res.SHA256 = info.SHA256
	a.res.SourceSize = info.Size

	// звук
	if a.options.Sound != "" {
		file, _, e = a.fetch(ctx, "options.sound", a.options.Sound, "sound")
		if e != nil {
			return e
		}
		a.sound = &videotool.File{Path: file}
	}

	// водяной знак
	if a.options.Watermark != nil {
		file, _, e = a.fetch(ctx, "options.watermark.input", a.options.Watermark.Input, "watermark")
		if e != nil {
			return e
		}
		a.mark = &videotool.File{Path: file}
	}

	return nil
}

// probe — скачанные файлы прочитаны и проверены лимитами: видео
// принятого кодека в пределах длительности и размера, звук принятого
// кодека, водяной знак — картинка
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

// probeSource — оригинал
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

// probeSound — внешний звук, если есть
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

// probeMark — водяной знак, если есть
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

// probeError — файл, который ffprobe не прочитал: закончившийся контекст
// как есть, иначе код c
func (a *videoRun) probeError(ctx context.Context, c code.Code, message string, err error) *code.Error {
	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	return code.New(c, message, err)
}

// planOutputs — план запуска ffmpeg; обработанная шкала тоже в пределах
// длительности (Speed её меняет)
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

// encode — проход громкости для Options.Normalize, один запуск ffmpeg,
// потом каждый результат прочитан и проверен
func (a *videoRun) encode(ctx context.Context) *code.Error {
	t := time.Now()
	tools := a.engine.video

	// громкость
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

	// проверка результатов
	report, e := tools.Verify(ctx, a.plan, a.dir)
	if e != nil {
		return e
	}

	a.report = report
	return nil
}

// upload — всё в хранилище волнами: init и сегменты (с ключом —
// зашифрованные), превью-картинки и превью; потом media playlist
// нескольких вариантов (со строками EXT-X-KEY, поэтому после каждого
// сегмента и его IV); проверка, что всё на месте; результат в журнал;
// вход последним, тоже проверенный. Вход опубликован = всё опубликовано:
// job, упавшая раньше, входа не оставляет и может повториться с теми же
// ключами
func (a *videoRun) upload(ctx context.Context) *code.Error {
	a.renditionsOf()

	// волна 1: медиа и дополнения
	e := a.putAll(ctx, a.mediaFiles())
	if e != nil {
		return e
	}

	// волна 2: media playlist с IV волны 1
	lists, e := a.mediaPlaylists()
	if e != nil {
		return e
	}

	e = a.putAll(ctx, lists)
	if e != nil {
		return e
	}

	// вход и результат, который он завершает
	a.entryOf()
	a.result()

	// всё, кроме входа, на месте
	e = a.stored(ctx, a.required())
	if e != nil {
		return e
	}

	// результат сохраняется до того, как вход сделает job видимой
	e = a.remember(ctx, a.res)
	if e != nil {
		return e
	}

	// вход последним
	e = a.putAll(ctx, []upload{a.entry})
	if e != nil {
		return e
	}

	return a.stored(ctx, []string{a.output})
}

// renditionsOf — опубликованный вид каждого варианта HLS: несколько
// вариантов — каждый в <папка output>/<name>/ с index.m3u8, единственный
// вариант — рядом с output, это и есть его playlist
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

// mediaFiles — волна 1: init и сегменты каждого варианта, превью-картинки,
// превью
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

// mediaPlaylists — media playlist каждого варианта как публикуется: с
// ключом — строка EXT-X-KEY с IV своего сегмента над каждым EXTINF. При
// нескольких вариантах это волна 2; playlist единственного варианта — сам
// вход
func (a *videoRun) mediaPlaylists() ([]upload, *code.Error) {
	var list []upload

	for i, r := range a.renditions {
		text, err := os.ReadFile(filepath.Join(a.dir, a.plan.Renditions[i].File))
		if err != nil {
			return nil, code.New(code.InternalTemporaryError, "media playlist missing", err)
		}

		// строки ключа
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

// entryOf — вход: master нескольких вариантов, media playlist одного, mp4
// у Video
func (a *videoRun) entryOf() {
	switch {
	case a.hls == nil:
		a.entry = upload{key: a.output, file: filepath.Join(a.dir, a.plan.Renditions[0].File)}
		return
	case len(a.renditions) == 1:
		a.entry = upload{key: a.output, data: a.renditions[0].text}
		return
	}

	// master: каждый вариант измерен по опубликованному
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

// result — измеренный результат: варианты как опубликованы, превью,
// общий размер всех объектов
func (a *videoRun) result() {
	res := video.Result{
		ID:         a.id,
		Output:     a.output,
		SHA256:     a.res.SHA256,
		SourceSize: a.res.SourceSize,
	}
	report := a.report

	// варианты
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

	// master нескольких вариантов
	if a.hls != nil && len(a.renditions) > 1 {
		res.Size += int64(len(a.entry.data))
	}

	// дополнения
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

// profileResult — измерения одного результата; Size — файл (вариант HLS
// ставит свой)
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

// required — объекты, проверяемые до входа: у каждого варианта media
// playlist (если вариантов несколько), init, первый и последний сегмент;
// превью-картинки; превью
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

// done — успешный результат
func (a *videoRun) done() (video.Result, error) {
	a.res.Elapsed = a.elapsed()
	a.work.done("renditions", len(a.res.Profiles), "duration", a.res.Duration)

	return a.res, nil
}

// again — сохранённый результат job, уже выполненной раньше
func (a *videoRun) again(stored []byte) (video.Result, error) {
	var res video.Result
	err := json.Unmarshal(stored, &res)
	if err != nil {
		return a.fail(code.New(code.InternalTemporaryError, "ledger result is not json", err))
	}

	a.work.again()
	return res, nil
}

// fail — упавший результат: id и ошибка, больше ничего не считается
func (a *videoRun) fail(e *code.Error) (video.Result, error) {
	a.res = video.Result{ID: a.id, Elapsed: a.elapsed(), Error: e}
	a.work.fail(e)

	return a.res, e
}
