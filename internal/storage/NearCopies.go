package storage

// Near-copies on a group page (Part 2, spec "Duplicates", D11): someone's
// photo that looks like a photo another member already put in the group.

import (
	"math"
	"math/bits"
	"strconv"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

// A pair is a near-copy only when all three hold. dHash looks at grayscale,
// so two colourings of one design (Apple's "Mac Blue" and "Mac Pink"
// wallpapers) collide on it alone; colour and shape tell them apart.
const (
	nearCopyMaxDistance = 6    // dHash bits; tuned on the preview with real photos (D11)
	nearCopyMaxColour   = 40   // Euclidean distance between average colours, in RGB units
	nearCopyMaxAspect   = 0.03 // relative difference of width / height, after rotation
)

// NearCopy is one item that looks like another uploader's older one
type NearCopy struct {
	FileId    string // the newer item of the pair
	FileUser  int    // who uploaded it
	MatchId   string // someone else's item it looks like
	MatchUser int
	MatchAt   int64 // taken-at of the match, or its upload date
}

// nearCandidate is a file whose preview can be compared
type nearCandidate struct {
	file          models.File
	hash          uint64
	at            int64
	color         [3]float64
	hasColor      bool
	width, height int
}

// parseColour reads "#rrggbb"
func parseColour(s string) (c [3]float64, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return c, false
	}
	for i := range c {
		v, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return c, false
		}
		c[i] = float64(v)
	}
	return c, true
}

// similar reports whether a and b agree in average colour and in shape. A
// preview without a colour or a size cannot be told apart from a different
// picture, so it does not match: a missing number is not a pass.
func (a nearCandidate) similar(b nearCandidate) bool {
	if !a.hasColor || !b.hasColor || a.width <= 0 || a.height <= 0 || b.width <= 0 || b.height <= 0 {
		return false
	}
	dr, dg, db := a.color[0]-b.color[0], a.color[1]-b.color[1], a.color[2]-b.color[2]
	if math.Sqrt(dr*dr+dg*dg+db*db) > nearCopyMaxColour {
		return false
	}
	ra, rb := float64(a.width)/float64(a.height), float64(b.width)/float64(b.height)
	return math.Abs(ra-rb) <= nearCopyMaxAspect*math.Max(ra, rb)
}

// NearCopies: for each of userId's files in files with a ready preview and a
// non-zero PHash, the closest older item from another uploader that is within
// the dHash distance and also close in average colour and aspect ratio,
// unless kept (Keep both). Exact SHA1 twins are not near-copies.
//
// With all set it is every such pair in files, whoever uploaded the newer
// item, for the group's creator and the admin; userId is then not used.
//
// Only the newer item of a pair is flagged (spec: "the new item's perceptual
// hash is compared with the rest of the group's"): newer is the later
// UploadDate, the larger id breaking a tie. The older item's uploader never
// sees the pair, so the newer uploader's "Keep both" settles it for good.
//
// The result follows the order of files; on equal distance the match that
// comes first in files wins.
func NearCopies(files []models.File, userId int, kept map[string]bool, all bool) []NearCopy {
	result := make([]NearCopy, 0)
	if userId == 0 && !all {
		return result
	}
	candidates := make([]nearCandidate, 0, len(files))
	for _, f := range files {
		p, ok := database.GetPreview(f.SHA1)
		if !ok || p.Status != models.PreviewReady || p.PHash == 0 {
			continue
		}
		at := p.TakenAt
		if at == 0 {
			at = f.UploadDate
		}
		colour, hasColour := parseColour(p.Color)
		candidates = append(candidates, nearCandidate{file: f, hash: p.PHash, at: at,
			color: colour, hasColor: hasColour, width: p.Width, height: p.Height})
	}
	for _, mine := range candidates {
		if (!all && mine.file.UserId != userId) || kept[mine.file.Id] {
			continue
		}
		best, bestDistance := -1, nearCopyMaxDistance+1
		for i, other := range candidates {
			if other.file.UserId == mine.file.UserId || other.file.SHA1 == mine.file.SHA1 ||
				!isNewer(mine.file, other.file) || !mine.similar(other) {
				continue
			}
			if d := bits.OnesCount64(mine.hash ^ other.hash); d < bestDistance {
				best, bestDistance = i, d
			}
		}
		if best < 0 {
			continue
		}
		match := candidates[best]
		result = append(result, NearCopy{FileId: mine.file.Id, FileUser: mine.file.UserId, MatchId: match.file.Id,
			MatchUser: match.file.UserId, MatchAt: match.at})
	}
	return result
}

// isNewer reports whether a was put in after b: the later upload date, the
// larger id breaking a tie
func isNewer(a, b models.File) bool {
	if a.UploadDate != b.UploadDate {
		return a.UploadDate > b.UploadDate
	}
	return a.Id > b.Id
}
