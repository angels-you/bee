package engine

import (
	"context"
	"fmt"
	"github.com/angels-you/bee/pkg/proto"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	imagetool "github.com/angels-you/bee/internal/engine/images"
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
)

// imagesRun is the state of one image job
type imagesRun struct {
	*work
	job     images.Job
	res     images.Result
	source  string             // downloaded file
	norm    imagetool.Source   // decoded source, every result is made from it
	outputs []imagetool.Output // in task order
}

// Images runs one image job: check → download → header, limits and
// a single decode → each result in order, checked →
// upload of all → result. Results are uploaded only when all
// are made, so a job that fails before upload does not touch storage; a retry
// overwrites. A failed job returns a Result with Error and the same
// *code.Error as the error
func (a *Engine) Images(ctx context.Context, j images.Job) (images.Result, error) {
	r := &imagesRun{work: a.newWork(proto.Images, j.ID, j.Input, j.UserMeta), job: j}

	// the meta of the caller
	e := r.checkMeta()
	if e != nil {
		return r.fail(e)
	}

	// check a copy: the caller's job is not changed
	r.job.Tasks = slices.Clone(j.Tasks)
	e = a.checkImages(&r.job)
	if e != nil {
		return r.fail(e)
	}

	// slot, timeout, folder
	ctx, end, e := r.begin(ctx, a.cfg.Images.Timeout)
	if e != nil {
		return r.fail(e)
	}
	defer end()

	// work
	e = r.download(ctx)
	if e != nil {
		return r.fail(e)
	}

	e = r.decode(ctx)
	if e != nil {
		return r.fail(e)
	}

	e = r.render(ctx)
	if e != nil {
		return r.fail(e)
	}

	e = r.upload(ctx)
	if e != nil {
		return r.fail(e)
	}

	return r.done()
}

// checkImages checks the job without limits (the type's Validate), then bee limits and
// defaults for empty profile parameters
func (a *Engine) checkImages(j *images.Job) *code.Error {
	e := j.Validate()
	if e != nil {
		return e
	}

	limit := a.cfg.Images.MaxDimension
	for i := range j.Tasks {
		p := &j.Tasks[i].Profile

		// box no larger than the limit
		switch {
		case p.MaxWidth > limit:
			return code.New(code.InvalidDimensions, fmt.Sprintf("task %s: max_width must be within 0..%d", j.Tasks[i].ID, limit), nil)
		case p.MaxHeight > limit:
			return code.New(code.InvalidDimensions, fmt.Sprintf("task %s: max_height must be within 0..%d", j.Tasks[i].ID, limit), nil)
		}

		// bee defaults
		p.Fit = p.FitOrInside()
		if p.MinQuality == 0 {
			p.MinQuality = a.cfg.Images.MinQuality
		}

		if p.Effort == nil {
			effort := a.cfg.Images.AvifEffort
			p.Effort = &effort
		}
	}

	return nil
}

// download fetches the source to <dir>/source
func (a *imagesRun) download(ctx context.Context) *code.Error {
	file, info, e := a.fetch(ctx, "input", a.job.Input, "source")
	if e != nil {
		return e
	}

	a.source = file
	a.res.SourceSize = info.Size
	a.res.SHA256 = info.SHA256
	return nil
}

// decode reads the header, checks the pixel limit, then decodes the source once
func (a *imagesRun) decode(ctx context.Context) *code.Error {
	t := time.Now()
	defer a.took(&a.processMs, t)

	tools := a.engine.images
	cfg := a.engine.cfg.Images

	// header
	info, err := tools.Probe(ctx, a.source)
	if err != nil {
		return code.As(err)
	}

	w, h := info.Upright()
	a.res.SourceFormat = info.Format
	a.res.SourceWidth = w
	a.res.SourceHeight = h
	a.log = a.log.With("source_size", a.res.SourceSize, "source_format", info.Format.String(), "source_width", w, "source_height", h)

	// pixel limit
	limit := int64(cfg.MaxMegapixels) * 1_000_000
	if int64(w)*int64(h) > limit {
		return code.New(code.PixelLimitExceeded, fmt.Sprintf("source %dx%d is above %d megapixels", w, h, cfg.MaxMegapixels), nil)
	}

	// decode
	a.norm, err = tools.Normalize(ctx, a.source, info, filepath.Join(a.dir, "source.tif"))
	if err != nil {
		return code.As(err)
	}

	// only the decoded copy is needed from here on
	os.Remove(a.source)
	return nil
}

// render makes each result in task order, one at a time; the first error
// fails the job
func (a *imagesRun) render(ctx context.Context) *code.Error {
	cfg := a.engine.cfg.Images

	for i, task := range a.job.Tasks {
		t := time.Now()

		// result
		out, err := a.engine.images.Render(ctx, a.norm, task.Profile, a.dir, strconv.Itoa(i), task.Profile.MinQuality, cfg.MaxDimension)
		if err != nil {
			e := code.As(err)
			return code.New(e.Code, "task "+task.ID+": "+e.Message, e.Err)
		}

		// timing
		elapsed := time.Since(t)
		a.processing(t)

		// task result
		a.outputs = append(a.outputs, out)
		a.res.Tasks = append(a.res.Tasks, images.TaskResult{
			ID:            task.ID,
			Name:          task.Name,
			Output:        task.Output,
			Format:        out.Format,
			Width:         out.Width,
			Height:        out.Height,
			Size:          out.Size,
			Quality:       out.Quality,
			SHA256:        out.SHA256,
			Thumbhash:     out.Colors.Thumbhash,
			DominantColor: out.Colors.Dominant,
			Palette:       out.Colors.Palette,
			AverageColor:  out.Colors.Average,
			Elapsed:       elapsed.Milliseconds(),
		})
	}

	return nil
}

// upload uploads all results in one wave
func (a *imagesRun) upload(ctx context.Context) *code.Error {
	list := make([]upload, 0, len(a.outputs))
	for i, out := range a.outputs {
		list = append(list, upload{
			key:  a.res.Tasks[i].Output,
			file: out.File,
			mime: out.Format.Mime(),
		})
	}

	return a.putAll(ctx, list)
}

// done returns a successful result
func (a *imagesRun) done() (images.Result, error) {
	a.res.ID = a.job.ID
	a.res.UserMeta = a.job.UserMeta
	a.res.Elapsed = a.elapsed()
	a.work.done("outputs", len(a.res.Tasks))

	return a.res, nil
}

// fail returns a failed result: no tasks, what is known about the source, the error
func (a *imagesRun) fail(e *code.Error) (images.Result, error) {
	a.res.ID = a.job.ID
	a.res.UserMeta = a.job.UserMeta
	a.res.Tasks = nil
	a.res.Elapsed = a.elapsed()
	a.res.Error = e
	a.work.fail(e)

	return a.res, e
}
