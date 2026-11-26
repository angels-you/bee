package images

import "github.com/angels-you/bee/pkg/types/code"

// Result is the result of a job. A failed job has Error and no Tasks: none
// of its results count, whatever is already in storage. The original's
// fields are filled as far as the job got: size and sha256 after the
// download, format and pixel size after reading the header (already with
// EXIF rotation applied)
type Result struct {
	ID           string       `json:"id"`
	Tasks        []TaskResult `json:"tasks,omitempty"`
	Elapsed      int64        `json:"elapsed"` // milliseconds for the whole job
	SHA256       string       `json:"sha256,omitempty"`
	SourceSize   int64        `json:"source_size,omitempty"`   // bytes
	SourceFormat Format       `json:"source_format,omitempty"` // jpg, png, webp, avif, heic, heif, tiff, gif
	SourceWidth  int          `json:"source_width,omitempty"`
	SourceHeight int          `json:"source_height,omitempty"`
	Error        *code.Error  `json:"error,omitempty"`
}

// TaskResult is one saved result, in Job.Tasks order. Quality is the
// quality used (below the requested one if max_bytes lowered it; 0 for png
// and lossless). Colors and thumbhash only when the profile asked for
// them; colors are #rrggbb
type TaskResult struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Output        string   `json:"output"` // key as saved, without a leading "/"
	Format        Format   `json:"format"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Size          int64    `json:"size"` // bytes
	Quality       int      `json:"quality"`
	SHA256        string   `json:"sha256"`
	Thumbhash     []byte   `json:"thumbhash,omitempty"` // base64 in json
	DominantColor string   `json:"dominant_color,omitempty"`
	Palette       []string `json:"palette,omitempty"`
	AverageColor  string   `json:"average_color,omitempty"`
	Elapsed       int64    `json:"elapsed"` // milliseconds for this result
}
