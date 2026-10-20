package storage

// Near-copies on a group page (Part 2, spec "Duplicates", D11): someone's
// photo that looks like a photo another member already put in the group.

import (
	"math/bits"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

const nearCopyMaxDistance = 6 // dHash bits; tuned on the preview with real photos (D11)

// NearCopy is one of the viewer's items that looks like someone else's
type NearCopy struct {
	FileId    string // the viewer's own item
	MatchId   string // someone else's item it looks like
	MatchUser int
	MatchAt   int64 // taken-at of the match, or its upload date
}

// nearCandidate is a file whose preview can be compared
type nearCandidate struct {
	file models.File
	hash uint64
	at   int64
}

// NearCopies: for each of userId's files in files with a ready preview and a
// non-zero PHash, the closest older item from another uploader within the
// threshold, unless kept (Keep both). Exact SHA1 twins are not near-copies.
//
// Only the newer item of a pair is flagged (spec: "the new item's perceptual
// hash is compared with the rest of the group's"): newer is the later
// UploadDate, the larger id breaking a tie. The older item's uploader never
// sees the pair, so the newer uploader's "Keep both" settles it for good.
//
// The result follows the order of files; on equal distance the match that
// comes first in files wins.
func NearCopies(files []models.File, userId int, kept map[string]bool) []NearCopy {
	result := make([]NearCopy, 0)
	if userId == 0 {
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
		candidates = append(candidates, nearCandidate{file: f, hash: p.PHash, at: at})
	}
	for _, mine := range candidates {
		if mine.file.UserId != userId || kept[mine.file.Id] {
			continue
		}
		best, bestDistance := -1, nearCopyMaxDistance+1
		for i, other := range candidates {
			if other.file.UserId == userId || other.file.SHA1 == mine.file.SHA1 || !isNewer(mine.file, other.file) {
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
		result = append(result, NearCopy{FileId: mine.file.Id, MatchId: match.file.Id,
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
