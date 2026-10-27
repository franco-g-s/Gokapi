//go:build test

package preview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// Fixtures are generated with the same tools the server uses, so nobody's
// real photos are committed. Missing tools are a hard failure, not a skip:
// a green run without vips would prove nothing.
func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func fixtures(t *testing.T) string {
	dir := t.TempDir()
	run(t, "vips", "black", filepath.Join(dir, "b.v"), "400", "300", "--bands", "3")
	run(t, "vips", "linear", filepath.Join(dir, "b.v"), filepath.Join(dir, "rot.jpg"), "1", "120")
	// Landscape pixels, orientation 6 => displays portrait 300x400
	run(t, "exiftool", "-q", "-overwrite_original", "-Orientation#=6",
		"-DateTimeOriginal=2026:08:14 14:32:05", "-OffsetTimeOriginal=+02:00", filepath.Join(dir, "rot.jpg"))
	run(t, "vips", "copy", filepath.Join(dir, "rot.jpg"), filepath.Join(dir, "photo.heic[compression=hevc]"))
	run(t, "exiftool", "-q", "-overwrite_original", "-DateTimeOriginal=2026:08:14 14:32:05",
		"-OffsetTimeOriginal=+02:00", filepath.Join(dir, "photo.heic"))
	run(t, "ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=10",
		"-metadata", "com.apple.quicktime.creationdate=2026-08-14T14:33:00+0200",
		"-movflags", "use_metadata_tags", filepath.Join(dir, "clip.mov"))
	run(t, "ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=1:size=2160x3840:rate=1",
		filepath.Join(dir, "portrait.mp4"))
	test.IsNil(t, os.WriteFile(filepath.Join(dir, "evil.mp4"),
		[]byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nfile:///etc/passwd\n#EXT-X-ENDLIST\n"), 0600))
	test.IsNil(t, os.WriteFile(filepath.Join(dir, "corrupt.jpg"), []byte("not an image at all"), 0600))
	return dir
}

func TestGenerate(t *testing.T) {
	dir := fixtures(t)
	out := t.TempDir()
	ctx := context.Background()
	want := time.Date(2026, 8, 14, 12, 32, 5, 0, time.UTC).Unix()

	jpg, err := Generate(ctx, filepath.Join(dir, "rot.jpg"), out, "sharot", models.PreviewKindImage)
	test.IsNil(t, err)
	test.IsEqualString(t, jpg.Status, models.PreviewReady)
	test.IsEqualBool(t, jpg.Height > jpg.Width, true) // rotated to portrait
	test.IsEqualInt(t, int(jpg.TakenAt), int(want))
	test.IsEqualBool(t, len(jpg.Color) == 7, true)
	thumb, view, _ := Paths(out, "sharot")
	test.FileExists(t, thumb)
	test.FileExists(t, view)

	heic, err := Generate(ctx, filepath.Join(dir, "photo.heic"), out, "shaheic", models.PreviewKindImage)
	test.IsNil(t, err)
	test.IsEqualBool(t, heic.Height > heic.Width, true) // not double-rotated
	test.IsEqualInt(t, int(heic.TakenAt), int(want))

	mov, err := Generate(ctx, filepath.Join(dir, "clip.mov"), out, "shamov", models.PreviewKindVideo)
	test.IsNil(t, err)
	test.IsEqualInt(t, int(mov.TakenAt), int(time.Date(2026, 8, 14, 12, 33, 0, 0, time.UTC).Unix()))
	test.IsEqualInt(t, int(mov.DurationMs), 2000)
	test.IsEqualInt(t, mov.Width, 320)
	_, _, poster := Paths(out, "shamov")
	test.FileExists(t, poster)

	_, err = Generate(ctx, filepath.Join(dir, "corrupt.jpg"), out, "shabad", models.PreviewKindImage)
	test.IsNotNil(t, err)
	noOutputs(t, out, "shabad")

	// A renamed HLS playlist must be refused, not interpreted.
	_, err = Generate(ctx, filepath.Join(dir, "evil.mp4"), out, "shaevil", models.PreviewKindVideo)
	test.IsNotNil(t, err)
	noOutputs(t, out, "shaevil")

	// Portrait video: the long edge is capped, not just the width.
	por, err := Generate(ctx, filepath.Join(dir, "portrait.mp4"), out, "shapor", models.PreviewKindVideo)
	test.IsNil(t, err)
	test.IsEqualBool(t, por.Height <= 1600 && por.Height > por.Width, true)

	// A large photo: view long edge capped at 1600, thumbnail at 480 wide.
	run(t, "vips", "black", filepath.Join(dir, "big.v"), "3000", "2000", "--bands", "3")
	run(t, "vips", "copy", filepath.Join(dir, "big.v"), filepath.Join(dir, "big.jpg"))
	big, err := Generate(ctx, filepath.Join(dir, "big.jpg"), out, "shabig", models.PreviewKindImage)
	test.IsNil(t, err)
	test.IsEqualInt(t, big.Width, 1600)
	test.IsEqualInt(t, big.Height, 1067)
	bigThumb, _, _ := Paths(out, "shabig")
	tw, _, err := dimensions(ctx, bigThumb)
	test.IsNil(t, err)
	test.IsEqualInt(t, tw, 480)
}

func noOutputs(t *testing.T, out, sha string) {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(out, sha+"-*"))
	test.IsNil(t, err)
	if len(left) != 0 {
		t.Fatalf("partial outputs left behind: %v", left)
	}
}

func TestKind(t *testing.T) {
	test.IsEqualString(t, Kind("image/heic", "IMG_1.HEIC"), models.PreviewKindImage)
	test.IsEqualString(t, Kind("application/octet-stream", "IMG_1.HEIC"), models.PreviewKindImage)
	test.IsEqualString(t, Kind("video/quicktime", "IMG_2.MOV"), models.PreviewKindVideo)
	test.IsEqualString(t, Kind("image/svg+xml", "x.svg"), "")
	test.IsEqualString(t, Kind("application/pdf", "x.pdf"), "")
}
