package images

import "github.com/angels-you/bee/pkg/types/code"

// Result — результат job. У упавшей job есть Error и нет Tasks: ни один
// её результат не считается, что бы уже ни лежало в хранилище. Поля
// оригинала заполнены, насколько job дошла: размер и sha256 после
// скачивания, формат и размер в пикселях после чтения заголовка (уже с
// учётом EXIF поворота)
type Result struct {
	ID           string       `json:"id"`
	Tasks        []TaskResult `json:"tasks,omitempty"`
	Elapsed      int64        `json:"elapsed"` // миллисекунд на всю job
	SHA256       string       `json:"sha256,omitempty"`
	SourceSize   int64        `json:"source_size,omitempty"`   // байт
	SourceFormat Format       `json:"source_format,omitempty"` // jpg, png, webp, avif, heic, heif, tiff, gif
	SourceWidth  int          `json:"source_width,omitempty"`
	SourceHeight int          `json:"source_height,omitempty"`
	Error        *code.Error  `json:"error,omitempty"`
}

// TaskResult — один сохранённый результат, в порядке Job.Tasks. Quality
// — использованное качество (ниже заказанного, если max_bytes его
// снизил; 0 у png и lossless). Цвета и thumbhash — только если профиль их
// просил; цвета #rrggbb
type TaskResult struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Output        string   `json:"output"` // ключ как сохранён, без "/" в начале
	Format        Format   `json:"format"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Size          int64    `json:"size"` // байт
	Quality       int      `json:"quality"`
	SHA256        string   `json:"sha256"`
	Thumbhash     []byte   `json:"thumbhash,omitempty"` // base64 в json
	DominantColor string   `json:"dominant_color,omitempty"`
	Palette       []string `json:"palette,omitempty"`
	AverageColor  string   `json:"average_color,omitempty"`
	Elapsed       int64    `json:"elapsed"` // миллисекунд на этот результат
}
