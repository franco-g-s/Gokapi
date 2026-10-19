package models

// Preview holds what the preview worker learned about one stored blob. It is
// keyed by SHA1 rather than file ID, because identical uploads share storage
// and therefore share their preview.
type Preview struct {
	Sha1       string `json:"sha1" redis:"sha1"`
	Status     string `json:"status" redis:"status"`
	Kind       string `json:"kind" redis:"kind"`
	Width      int    `json:"width" redis:"width"`     // after rotation
	Height     int    `json:"height" redis:"height"`   // after rotation
	TakenAt    int64  `json:"takenAt" redis:"takenat"` // UTC unix seconds, 0 if unknown
	DurationMs int64  `json:"durationMs" redis:"durationms"`
	Color      string `json:"color" redis:"color"` // "#rrggbb" average colour
	PHash      uint64 `json:"-" redis:"phash"`     // 64-bit dHash, used by Part 2
	Attempts   int    `json:"-" redis:"attempts"`
	Updated    int64  `json:"-" redis:"updated"`
}

const (
	PreviewPending     = "pending"
	PreviewReady       = "ready"
	PreviewFailed      = "failed"
	PreviewUnsupported = "unsupported"
	PreviewKindImage   = "image"
	PreviewKindVideo   = "video"
)

// Version is the cache-busting token placed in preview URLs.
func (p Preview) Version() string {
	if len(p.Sha1) < 12 {
		return p.Sha1
	}
	return p.Sha1[:12]
}
