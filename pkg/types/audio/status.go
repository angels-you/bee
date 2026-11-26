package audio

// Status — этап работы job
type Status string

const (
	StatusPending     Status = "pending"
	StatusDownloading Status = "downloading"
	StatusProbing     Status = "probing"
	StatusAnalyzing   Status = "analyzing"
	StatusNormalizing Status = "normalizing"
	StatusEncoding    Status = "encoding"
	StatusWaveform    Status = "waveform"
	StatusUploading   Status = "uploading"
	StatusVerifying   Status = "verifying"
	StatusReady       Status = "ready"
	StatusFailed      Status = "failed"
)

// Percent — отметка прогресса этапа, 0..100, в порядке работы
func (a Status) Percent() int {
	switch a {
	case StatusDownloading:
		return 5
	case StatusProbing:
		return 15
	case StatusAnalyzing:
		return 25
	case StatusNormalizing:
		return 40
	case StatusEncoding:
		return 45
	case StatusWaveform:
		return 70
	case StatusUploading:
		return 80
	case StatusVerifying:
		return 95
	case StatusReady:
		return 100
	case StatusFailed:
		return 100
	}

	return 0
}

// Progress — событие прогресса работающей job
type Progress struct {
	ID       string `json:"id"`
	Status   Status `json:"status"`
	Progress int    `json:"progress"` // 0..100
	Message  string `json:"message,omitempty"`
}
