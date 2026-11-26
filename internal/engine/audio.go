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

// audioRun — состояние одной аудио job
type audioRun struct {
	*work
	job audio.Job
	res audio.Result
	hls *encrypt.HLS // шифрование сегментов; nil без ключа

	source     string // скачанный оригинал
	meta       audio.Metadata
	analysis   audiotool.Analysis
	gain       float64
	renditions []audioRendition // в порядке Options.Renditions
	files      []upload         // init, сегменты и waveform json
}

// audioRendition — один битрейт HLS: файлы ffmpeg и что из них опубликовано
type audioRendition struct {
	kbit     int
	folder   string // имя папки: битрейт в kbit/s
	dir      string // локально, как записал ffmpeg
	key      string // папка в хранилище: <папка master>/<folder>
	playlist audiotool.Playlist
	track    *track
}

// Audio — одна аудио job: проверка и дефолты → слот → master ещё не
// опубликован (иначе ALREADY_EXISTS или сохранённый результат этой же
// job) → скачивание → ffprobe и лимиты → анализ → fingerprint →
// громкость → HLS всех вариантов → waveform → загрузка волнами, master
// последним → проверка. Упавшая job возвращает Result с Error и тот же
// *code.Error как ошибку
func (a *Engine) Audio(ctx context.Context, j audio.Job) (audio.Result, error) {
	r := &audioRun{work: a.newWork(proto.Audio, j.ID, j.Input), job: j}
	r.res.ID = j.ID
	r.res.Metrics = map[string]int64{}

	// проверка на копии: job вызывающего не меняется
	r.job.Options.Renditions = slices.Clone(j.Options.Renditions)
	e := a.checkAudio(&r.job)
	if e != nil {
		return r.fail(e)
	}
	r.output = r.job.Output
	r.log = r.log.With("output", r.job.Output)

	// шифрование
	if len(r.job.Key) > 0 {
		hls, err := encrypt.New(r.job.Key)
		if err != nil {
			return r.fail(code.New(code.InvalidJob, err.Error(), nil))
		}
		r.hls = hls
	}

	// слот, таймаут, папка
	ctx, end, e := r.begin(ctx, a.cfg.Audio.Timeout)
	if e != nil {
		return r.fail(e)
	}
	defer end()

	// master уже опубликован: отказ или эта job уже выполнена
	stored, done, e := r.published(ctx)
	if e != nil {
		return r.fail(e)
	}
	if done {
		return r.again(stored)
	}

	// работа
	e = r.pipeline(ctx)
	if e != nil {
		return r.fail(e)
	}

	return r.done()
}

// checkAudio — job без настроек (Validate типа), дефолты пустых
// параметров из конфига, потом то, что зависит от них: папки вариантов
func (a *Engine) checkAudio(j *audio.Job) *code.Error {
	e := j.Validate()
	if e != nil {
		return e
	}

	// дефолты bee
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

	// папка каждого варианта рядом с master принадлежит HLS
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

// pipeline — этапы по порядку, время каждого в Result.Metrics
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

// stage — один этап; его время в Result.Metrics под name
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

// warn — некритичное замечание о качестве
func (a *audioRun) warn(c, message string) {
	a.res.Warnings = append(a.res.Warnings, audio.Warning{Code: c, Message: message})
}

// failed — ошибка инструмента: закончившийся контекст как есть, иначе код c
func (a *audioRun) failed(ctx context.Context, c code.Code, message string, err error) *code.Error {
	if ctx.Err() != nil {
		return code.As(ctx.Err())
	}

	return code.New(c, message+": "+a.short(err), err)
}

// download — оригинал в <dir>/source
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

// probe — ffprobe, формат и лимиты
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

	// формат и длительность
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

	// замечания о качестве оригинала
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

// analyze — один проход декодирования: громкость, качество, pcm для waveform
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

	// анализ
	res, err := a.engine.audio.Analyze(ctx, a.source, o)
	if err != nil {
		return a.failed(ctx, code.InvalidAudio, "audio analysis failed", err)
	}

	// ни одного сэмпла выше пола: цифровая тишина на весь трек или нет
	// сэмплов вовсе — нечего кодировать и публиковать
	if res.Loudness.SamplePeak <= audiotool.LevelFloor {
		return code.New(code.InvalidAudio, "track is silent", nil)
	}

	a.analysis = res
	a.res.Loudness = &a.analysis.Loudness

	// громкость
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

	// качество
	a.res.Quality = &a.analysis.Quality
	a.qualityWarnings(o.DurationSec)
	return nil
}

// qualityWarnings — замечания по диагностике качества
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

	// длинная тишина внутри трека
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

// fingerprint — chromaprint по желанию; ошибка — только замечание
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

// normalize — только линейное усиление
func (a *audioRun) normalize(ctx context.Context) *code.Error {
	o := a.job.Options
	if !o.Normalize {
		return nil
	}

	gain, limited := audiotool.ComputeGain(a.analysis.Loudness, o.LUFS, o.TruePeak)
	a.gain = gain
	a.analysis.Loudness.Gain = gain
	a.analysis.Loudness.GainLimited = limited

	if limited {
		a.warn("gain_limited", fmt.Sprintf("gain reduced to %.2f dB to keep true peak under %.1f dBTP", gain, o.TruePeak))
	}

	return nil
}

// encode — все варианты одним запуском ffmpeg, потом media playlist
// каждого проверены и выровнены, и громкость результата
func (a *audioRun) encode(ctx context.Context) *code.Error {
	t := time.Now()
	defer a.processing(t)

	o := a.job.Options
	tools := a.engine.audio
	dir := filepath.Join(a.dir, "hls")

	// кодирование
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

	// плеер переключает варианты на границах сегментов: каждый вариант
	// должен быть порезан ровно как первый
	for _, kbit := range o.Renditions {
		e := a.rendition(ctx, dir, kbit)
		if e != nil {
			return e
		}
	}

	// громкость результата
	a.measure(ctx)

	// init и сегменты каждого варианта
	for i := range a.renditions {
		r := &a.renditions[i]
		a.files = append(a.files, upload{key: path.Join(r.key, r.playlist.Init), file: filepath.Join(r.dir, r.playlist.Init), track: r.track})

		for _, s := range r.playlist.Segments {
			a.files = append(a.files, upload{key: path.Join(r.key, s.Name), file: filepath.Join(r.dir, s.Name), track: r.track, encrypt: a.hls})
		}
	}

	return nil
}

// rendition — media playlist варианта kbit проверен и совпадает с первым
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

// measure — громкость того, что слушатель услышит первым: первый вариант,
// с него начинает плеер; остальные — тот же сигнал в другом битрейте
func (a *audioRun) measure(ctx context.Context) {
	first := filepath.Join(a.renditions[0].dir, audiotool.PlaylistName)

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

// waveform — json в Job.Wave и Result.Waveform (его пики) из pcm анализа
func (a *audioRun) waveform(ctx context.Context) *code.Error {
	if a.job.Wave == "" {
		return nil
	}

	o := a.job.Options
	pcm := filepath.Join(a.dir, "pcm.raw")

	// pcm прохода анализа
	f, err := os.Open(pcm)
	if err != nil {
		return code.New(code.InternalTemporaryError, "pcm missing", err)
	}

	w, err := audiotool.BuildWaveform(f, a.meta.SampleRate, o.WaveformPPS, a.gain)
	f.Close()
	os.Remove(pcm)
	if err != nil {
		return code.New(code.InvalidAudio, "waveform: "+a.short(err), err)
	}

	// json и пики
	js, err := json.Marshal(w)
	if err != nil {
		return code.New(code.InternalTemporaryError, "waveform json", err)
	}

	a.files = append(a.files, upload{key: a.job.Wave, data: js})
	a.res.Waveform = w.Peaks(o.WaveformPoints)
	return nil
}

// upload — волнами, master последним: init, сегменты и waveform json;
// json метаданных и анализа (метаданные перечисляют варианты, измеренные
// по опубликованным сегментам); media playlist; результат в журнал;
// master. Опубликованный master — полный набор: при любой ошибке он не
// публикуется. С ключом сегменты шифруются, а media playlist получают
// строки ключа; в master ключа нет
func (a *audioRun) upload(ctx context.Context) *code.Error {
	a.res.Encrypted = a.hls != nil

	// волна 1: файлы
	e := a.putAll(ctx, a.files)
	if e != nil {
		return e
	}

	// media playlist и варианты результата
	lists, variants, e := a.playlists()
	if e != nil {
		return e
	}

	// волна 2: json
	side, e := a.sideFiles()
	if e != nil {
		return e
	}

	e = a.putAll(ctx, side)
	if e != nil {
		return e
	}

	// волна 3: media playlist
	e = a.putAll(ctx, lists)
	if e != nil {
		return e
	}

	// результат в журнал до master
	a.res.Size = a.uploaded
	a.res.Elapsed = a.elapsed()
	e = a.remember(ctx, a.res)
	if e != nil {
		return e
	}

	// master последним: кто видит его, видит полный набор
	master := upload{key: a.job.Output, data: []byte(audiotool.Master(variants))}
	e = a.putAll(ctx, []upload{master})
	if e != nil {
		return e
	}

	a.res.Size = a.uploaded
	return nil
}

// playlists — media playlist каждого варианта как публикуется (с ключами)
// и варианты master и результата
func (a *audioRun) playlists() ([]upload, []audiotool.Variant, *code.Error) {
	lists := make([]upload, 0, len(a.renditions))
	variants := make([]audiotool.Variant, 0, len(a.renditions))

	for i := range a.renditions {
		r := &a.renditions[i]

		// текст ffmpeg
		text, err := os.ReadFile(filepath.Join(r.dir, audiotool.PlaylistName))
		if err != nil {
			return nil, nil, code.New(code.InternalTemporaryError, "playlist missing", err)
		}

		// строки ключа
		if a.hls != nil {
			published, err := a.hls.Playlist(string(text), a.job.KeyURI, r.track.ivs)
			if err != nil {
				return nil, nil, code.New(code.InternalTemporaryError, "playlist encryption: "+a.short(err), err)
			}
			text = []byte(published)
		}

		// измерено по опубликованному
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
			URI:              r.folder + "/" + audiotool.PlaylistName,
			Bandwidth:        peak,
			AverageBandwidth: average,
		})
		lists = append(lists, upload{key: path.Join(r.key, audiotool.PlaylistName), data: text})
	}

	return lists, variants, nil
}

// sideFiles — json метаданных и анализа, если job их просит
func (a *audioRun) sideFiles() ([]upload, *code.Error) {
	var side []upload

	// метаданные
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

	// анализ: только то, что безопасно публиковать
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

// verify — нужные объекты действительно на месте: master, у каждого
// варианта media playlist, init, первый и последний сегмент, и каждый
// json, который просила job
func (a *audioRun) verify(ctx context.Context) *code.Error {
	required := []string{a.job.Output}

	for _, r := range a.renditions {
		segments := r.playlist.Segments
		required = append(required,
			path.Join(r.key, audiotool.PlaylistName),
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

// short — одна строка ошибки для сообщений, не длиннее 300 байт
func (a *audioRun) short(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	if len(s) > 300 {
		s = s[:300]
	}

	return s
}

// done — успешный результат
func (a *audioRun) done() (audio.Result, error) {
	a.res.Elapsed = a.elapsed()
	a.res.Metrics["total"] = a.res.Elapsed
	a.work.done("renditions", len(a.res.Renditions))

	return a.res, nil
}

// again — сохранённый результат job, уже выполненной раньше
func (a *audioRun) again(stored []byte) (audio.Result, error) {
	var res audio.Result
	err := json.Unmarshal(stored, &res)
	if err != nil {
		return a.fail(code.New(code.InternalTemporaryError, "ledger result is not json", err))
	}

	a.work.again()
	return res, nil
}

// fail — упавший результат: id, ошибка и что известно об оригинале
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
