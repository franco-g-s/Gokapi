// Package preview turns stored uploads into display-only thumbnails and
// previews by running vips and ffmpeg as subprocesses. Originals are only ever
// read, never modified.
package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/models"
)

const (
	thumbWidth  = 480
	viewMaxEdge = 1600
)

var imageExt = map[string]bool{".jpg": true, ".jpeg": true, ".heic": true, ".heif": true, ".png": true, ".webp": true, ".gif": true}
var videoExt = map[string]bool{".mov": true, ".mp4": true, ".m4v": true}

// Kind decides whether a file gets a preview, from its extension first
// (browsers often send HEIC as application/octet-stream) then its MIME type.
func Kind(contentType, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case imageExt[ext]:
		return models.PreviewKindImage
	case videoExt[ext]:
		return models.PreviewKindVideo
	case contentType == "image/svg+xml":
		return ""
	case strings.HasPrefix(contentType, "image/"):
		return models.PreviewKindImage
	case strings.HasPrefix(contentType, "video/"):
		return models.PreviewKindVideo
	}
	return ""
}

// Paths returns where the outputs for a blob live
func Paths(dir, sha1 string) (thumb, view, poster string) {
	return filepath.Join(dir, sha1+"-thumb.webp"), filepath.Join(dir, sha1+"-view.jpg"), filepath.Join(dir, sha1+"-poster.webp")
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// VIPS_BLOCK_UNTRUSTED makes vips refuse loaders not marked safe for hostile input.
	cmd.Env = append(os.Environ(), "VIPS_CONCURRENCY=1", "VIPS_BLOCK_UNTRUSTED=1")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// Generate produces the display outputs for one blob and returns its metadata.
// On error, no partial outputs for this sha1 are left behind.
func Generate(ctx context.Context, src, outDir, sha1, kind string) (p models.Preview, err error) {
	if err = os.MkdirAll(outDir, 0700); err != nil {
		return models.Preview{}, err
	}
	defer func() {
		if err != nil {
			thumb, view, poster := Paths(outDir, sha1)
			for _, f := range []string{thumb, view, poster, filepath.Join(outDir, sha1+"-tiny.png")} {
				_ = os.Remove(f)
			}
		}
	}()
	switch kind {
	case models.PreviewKindImage:
		return generateImage(ctx, src, outDir, sha1)
	case models.PreviewKindVideo:
		return generateVideo(ctx, src, outDir, sha1)
	}
	return models.Preview{}, errors.New("unsupported kind")
}

func generateImage(parent context.Context, src, outDir, sha1 string) (models.Preview, error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	thumb, view, _ := Paths(outDir, sha1)
	// [0] = first page/frame only (GIF, multi-image HEIC). Thumbnail autorotates.
	_, err := command(ctx, "vips", "thumbnail", src+"[0]", view+"[Q=75,strip]",
		strconv.Itoa(viewMaxEdge), "--height", strconv.Itoa(viewMaxEdge), "--size", "down")
	if err != nil {
		return models.Preview{}, err
	}
	p := models.Preview{Sha1: sha1, Kind: models.PreviewKindImage, TakenAt: imageTakenAt(ctx, src)}
	return finish(ctx, p, view, thumb, outDir)
}

func generateVideo(parent context.Context, src, outDir, sha1 string) (models.Preview, error) {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	thumb, view, poster := Paths(outDir, sha1)
	// Uploads are untrusted: only local files, and only MOV/MP4 containers, so a
	// renamed playlist (hls, concat) cannot make ffmpeg fetch or read other files.
	// -f mov forces the MOV/MP4 demuxer on the probe too, so ffprobe never
	// auto-detects (and opens) anything else; allowedContainer stays as a check.
	probe, err := command(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file", "-print_format", "json",
		"-show_entries", "format=duration,format_name:format_tags:stream=codec_type,width,height:stream_side_data=rotation",
		"-f", "mov", src)
	if err != nil {
		return models.Preview{}, err
	}
	if !allowedContainer(probe) {
		return models.Preview{}, errors.New("unsupported video container")
	}
	p := models.Preview{Sha1: sha1, Kind: models.PreviewKindVideo}
	p.TakenAt, p.DurationMs = parseProbe(probe)
	// One frame at 1s (or the first frame for shorter clips), autorotated by ffmpeg.
	seek := "1"
	if p.DurationMs < 1500 {
		seek = "0"
	}
	// The scale caps the long edge (after autorotation), so portrait clips are bounded too.
	scale := fmt.Sprintf("scale='if(gt(iw,ih),min(%[1]d,iw),-2)':'if(gt(iw,ih),-2,min(%[1]d,ih))'", viewMaxEdge)
	_, err = command(ctx, "ffmpeg", "-loglevel", "error", "-threads", "1", "-y", "-protocol_whitelist", "file",
		"-ss", seek, "-f", "mov", "-i", src, "-frames:v", "1", "-vf", scale, view)
	if err != nil {
		return models.Preview{}, err
	}
	p, err = finish(ctx, p, view, thumb, outDir)
	if err != nil {
		return p, err
	}
	// The poster is the thumbnail of the frame; the board shows it with a play badge.
	if err := copyFile(thumb, poster); err != nil {
		return models.Preview{}, err
	}
	return p, nil
}

// finish derives dimensions, thumbnail, colour and hash from the rotated view.
func finish(ctx context.Context, p models.Preview, view, thumb, outDir string) (models.Preview, error) {
	w, h, err := dimensions(ctx, view)
	if err != nil {
		return models.Preview{}, err
	}
	p.Width, p.Height = w, h
	if _, err = command(ctx, "vips", "thumbnail", view, thumb+"[Q=75,strip]", strconv.Itoa(thumbWidth),
		"--height", "100000", "--size", "down"); err != nil {
		return models.Preview{}, err
	}
	tiny := filepath.Join(outDir, p.Sha1+"-tiny.png")
	defer os.Remove(tiny)
	if _, err = command(ctx, "vips", "thumbnail", view, tiny, "9", "--height", "8", "--size", "force"); err != nil {
		return models.Preview{}, err
	}
	p.Color, p.PHash, err = colourAndHash(tiny)
	if err != nil {
		return models.Preview{}, err
	}
	p.Status = models.PreviewReady
	p.Updated = time.Now().Unix()
	return p, nil
}

func dimensions(ctx context.Context, path string) (int, int, error) {
	out, err := command(ctx, "vipsheader", "-f", "width", path)
	if err != nil {
		return 0, 0, err
	}
	w, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	out, err = command(ctx, "vipsheader", "-f", "height", path)
	if err != nil {
		return 0, 0, err
	}
	h, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if w == 0 || h == 0 {
		return 0, 0, errors.New("zero dimensions")
	}
	return w, h, nil
}

// colourAndHash reads the 9x8 image: average colour, and a 64-bit difference
// hash (each bit: is this pixel brighter than its right neighbour).
func colourAndHash(path string) (string, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return "", 0, err
	}
	b := img.Bounds()
	var rs, gs, bs, n uint64
	luma := func(x, y int) uint32 {
		r, g, bl, _ := img.At(x, y).RGBA()
		return (299*r + 587*g + 114*bl) / 1000
	}
	var hash uint64
	bit := 0
	for y := b.Min.Y; y < b.Max.Y && y < b.Min.Y+8; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			rs, gs, bs, n = rs+uint64(r>>8), gs+uint64(g>>8), bs+uint64(bl>>8), n+1
			if x < b.Min.X+8 && x+1 < b.Max.X {
				if luma(x, y) > luma(x+1, y) {
					hash |= 1 << uint(bit)
				}
				bit++
			}
		}
	}
	if n == 0 {
		return "", 0, errors.New("empty image")
	}
	return fmt.Sprintf("#%02x%02x%02x", rs/n, gs/n, bs/n), hash, nil
}

// imageTakenAt reads EXIF capture time. Missing data is 0, never a guess.
func imageTakenAt(ctx context.Context, src string) int64 {
	field := func(name string) string {
		out, err := command(ctx, "vipsheader", "-f", name, src)
		if err != nil {
			return ""
		}
		// vips prints "2026:08:14 14:32:05 (2026:08:14 14:32:05, ASCII, ...)"
		s := strings.TrimSpace(string(out))
		if i := strings.Index(s, " ("); i > 0 {
			s = s[:i]
		}
		return s
	}
	raw := field("exif-ifd2-DateTimeOriginal")
	if raw == "" {
		return 0
	}
	loc := time.Local
	if off := field("exif-ifd2-OffsetTimeOriginal"); off != "" {
		if t, err := time.Parse("-07:00", off); err == nil {
			loc = t.Location()
		}
	}
	t, err := time.ParseInLocation("2006:01:02 15:04:05", raw, loc)
	if err != nil {
		return 0
	}
	return t.UTC().Unix()
}

// allowedContainer accepts only the MOV/MP4 demuxer family.
func allowedContainer(raw []byte) bool {
	var probe struct {
		Format struct {
			FormatName string `json:"format_name"`
		} `json:"format"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return false
	}
	n := probe.Format.FormatName
	return strings.Contains(n, "mov") || strings.Contains(n, "mp4")
}

func parseProbe(raw []byte) (takenAt, durationMs int64) {
	var probe struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return 0, 0
	}
	if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
		durationMs = int64(d*1000 + 0.5)
	}
	if s := probe.Format.Tags["com.apple.quicktime.creationdate"]; s != "" {
		if t, err := time.Parse("2006-01-02T15:04:05-0700", s); err == nil {
			return t.UTC().Unix(), durationMs
		}
	}
	if s := probe.Format.Tags["creation_time"]; s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC().Unix(), durationMs
		}
	}
	return 0, durationMs
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0600)
}
