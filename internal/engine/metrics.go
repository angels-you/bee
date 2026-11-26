package engine

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/angels-you/bee/pkg/types/code"
)

// Metrics holds bee counters in memory, WritePrometheus writes them in the
// Prometheus text format (GET /metrics). type is the job type: audio, stream,
// video, images.
//
//	bee_jobs_total{type}                 jobs accepted
//	bee_jobs_completed_total{type}       jobs completed
//	bee_jobs_failed_total{type,code}     jobs failed, by error code
//	bee_jobs_retried_total               redeliveries (counted by the nats transport, Retried)
//	bee_jobs_active                      jobs running now
//	bee_job_duration_seconds{type}       histogram of whole jobs
//	bee_processing_seconds{type}         histogram of processing (ffmpeg, vips)
//	bee_download_bytes_total             bytes downloaded
//	bee_upload_bytes_total               bytes uploaded
type Metrics struct {
	mu         sync.Mutex
	buckets    []float64 // histogram upper bounds, seconds
	jobs       map[string]int64
	completed  map[string]int64
	failed     map[string]int64 // type + "\x00" + code
	retried    int64
	active     int64
	duration   map[string]*histogram
	processing map[string]*histogram
	download   int64
	upload     int64
}

// histogram is a Prometheus histogram over the Metrics.buckets bounds
type histogram struct {
	counts []int64 // per bucket, not cumulative
	sum    float64
	count  int64
}

// init sets empty counters; buckets from fractions of a second to minutes
func (a *Metrics) init() {
	a.buckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}
	a.jobs = map[string]int64{}
	a.completed = map[string]int64{}
	a.failed = map[string]int64{}
	a.duration = map[string]*histogram{}
	a.processing = map[string]*histogram{}
}

// Retried counts one redelivered job (called by the nats transport)
func (a *Metrics) Retried() {
	a.mu.Lock()
	a.retried++
	a.mu.Unlock()
}

// started counts an incoming job of type kind
func (a *Metrics) started(kind string) {
	a.mu.Lock()
	a.jobs[kind]++
	a.active++
	a.mu.Unlock()
}

// finished records a job of type kind that ended after d: failed with code c or
// completed if c is empty
func (a *Metrics) finished(kind string, c code.Code, d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.active--
	if c == "" {
		a.completed[kind]++
	} else {
		a.failed[kind+"\x00"+string(c)]++
	}

	a.observe(a.duration, kind, d)
}

// processed records that processing (ffmpeg, vips) of a job of type kind took d
func (a *Metrics) processed(kind string, d time.Duration) {
	a.mu.Lock()
	a.observe(a.processing, kind, d)
	a.mu.Unlock()
}

// transferred adds bytes downloaded and bytes uploaded
func (a *Metrics) transferred(down, up int64) {
	a.mu.Lock()
	a.download += down
	a.upload += up
	a.mu.Unlock()
}

// observe adds d to the histogram of type kind; call under mu
func (a *Metrics) observe(set map[string]*histogram, kind string, d time.Duration) {
	h := set[kind]
	if h == nil {
		h = &histogram{counts: make([]int64, len(a.buckets))}
		set[kind] = h
	}

	v := d.Seconds()
	h.sum += v
	h.count++
	for i, le := range a.buckets {
		if v <= le {
			h.counts[i]++
			return
		}
	}
}

// WritePrometheus writes the Prometheus text format
func (a *Metrics) WritePrometheus(w io.Writer) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// jobs by type
	fmt.Fprintln(w, "# TYPE bee_jobs_total counter")
	for _, kind := range a.keys(a.jobs) {
		fmt.Fprintf(w, "bee_jobs_total{type=%q} %d\n", kind, a.jobs[kind])
	}

	fmt.Fprintln(w, "# TYPE bee_jobs_completed_total counter")
	for _, kind := range a.keys(a.completed) {
		fmt.Fprintf(w, "bee_jobs_completed_total{type=%q} %d\n", kind, a.completed[kind])
	}

	// errors by type and code
	fmt.Fprintln(w, "# TYPE bee_jobs_failed_total counter")
	for _, key := range a.keys(a.failed) {
		kind, c, _ := strings.Cut(key, "\x00")
		fmt.Fprintf(w, "bee_jobs_failed_total{type=%q,code=%q} %d\n", kind, c, a.failed[key])
	}

	fmt.Fprintf(w, "# TYPE bee_jobs_retried_total counter\nbee_jobs_retried_total %d\n", a.retried)
	fmt.Fprintf(w, "# TYPE bee_jobs_active gauge\nbee_jobs_active %d\n", a.active)

	// histograms
	a.writeHistograms(w, "bee_job_duration_seconds", a.duration)
	a.writeHistograms(w, "bee_processing_seconds", a.processing)

	// traffic
	fmt.Fprintf(w, "# TYPE bee_download_bytes_total counter\nbee_download_bytes_total %d\n", a.download)
	fmt.Fprintf(w, "# TYPE bee_upload_bytes_total counter\nbee_upload_bytes_total %d\n", a.upload)
}

// writeHistograms writes cumulative buckets, +Inf, sum and count for each type
func (a *Metrics) writeHistograms(w io.Writer, name string, set map[string]*histogram) {
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)

	kinds := make([]string, 0, len(set))
	for kind := range set {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	for _, kind := range kinds {
		h := set[kind]

		var cum int64
		for i, le := range a.buckets {
			cum += h.counts[i]
			fmt.Fprintf(w, "%s_bucket{type=%q,le=%q} %d\n", name, kind, strconv.FormatFloat(le, 'f', -1, 64), cum)
		}

		fmt.Fprintf(w, "%s_bucket{type=%q,le=\"+Inf\"} %d\n", name, kind, h.count)
		fmt.Fprintf(w, "%s_sum{type=%q} %s\n", name, kind, strconv.FormatFloat(h.sum, 'f', -1, 64))
		fmt.Fprintf(w, "%s_count{type=%q} %d\n", name, kind, h.count)
	}
}

// keys returns the counter keys in order
func (a *Metrics) keys(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)

	return out
}
