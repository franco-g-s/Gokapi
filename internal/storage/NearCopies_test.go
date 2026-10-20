//go:build test

package storage

import (
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// nearFile saves a group fixture with a ready preview of this dHash
func nearFile(t *testing.T, id string, owner int, sha string, phash uint64, takenAt int64) models.File {
	t.Helper()
	f := groupFile(id, owner, func(f *models.File) {
		if sha != "" {
			f.SHA1 = sha
		}
		f.UploadDate = 1000
	})
	database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage,
		Width: 4, Height: 3, TakenAt: takenAt, PHash: phash, Updated: 1})
	t.Cleanup(func() {
		database.DeletePreview(f.SHA1)
		dropGroupFiles(f)
	})
	return f
}

func TestNearCopies(t *testing.T) {
	const anna, ben = 41, 42
	const base = uint64(0xF0F0F0F0F0F0F0F0)

	t.Run("distance 3 to another uploader is flagged for the viewer's file", func(t *testing.T) {
		mine := nearFile(t, "nearmine3", anna, "", base^0b111, 0)
		theirs := nearFile(t, "neartheirs3", ben, "", base, 5000)
		got := NearCopies([]models.File{mine, theirs}, anna, nil)
		test.IsEqualInt(t, len(got), 1)
		test.IsEqualString(t, got[0].FileId, mine.Id)
		test.IsEqualString(t, got[0].MatchId, theirs.Id)
		test.IsEqualInt(t, got[0].MatchUser, ben)
		test.IsEqualInt64(t, got[0].MatchAt, 5000) // taken-at of the match
		// Ben sees the same pair from his side
		got = NearCopies([]models.File{mine, theirs}, ben, nil)
		test.IsEqualInt(t, len(got), 1)
		test.IsEqualString(t, got[0].FileId, theirs.Id)
		test.IsEqualInt64(t, got[0].MatchAt, 1000) // no taken-at: the upload date
	})

	t.Run("distance 6 is still flagged, 7 is not", func(t *testing.T) {
		mine := nearFile(t, "nearmine6", anna, "", base^0b111111, 0)
		theirs := nearFile(t, "neartheirs6", ben, "", base, 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, anna, nil)), 1)

		far := nearFile(t, "nearmine7", anna, "", base^0b1111111, 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{far, theirs}, anna, nil)), 0)
	})

	t.Run("the closest match wins", func(t *testing.T) {
		mine := nearFile(t, "nearminec", anna, "", base, 0)
		farther := nearFile(t, "nearfarc", ben, "", base^0b11111, 0)
		closer := nearFile(t, "nearclosec", ben, "", base^0b1, 0)
		got := NearCopies([]models.File{mine, farther, closer}, anna, nil)
		test.IsEqualInt(t, len(got), 1)
		test.IsEqualString(t, got[0].MatchId, closer.Id)
	})

	t.Run("the same owner is never flagged", func(t *testing.T) {
		a := nearFile(t, "nearsamea", anna, "", base, 0)
		b := nearFile(t, "nearsameb", anna, "", base^0b1, 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{a, b}, anna, nil)), 0)
	})

	t.Run("kept is not flagged", func(t *testing.T) {
		mine := nearFile(t, "nearminek", anna, "", base^0b1, 0)
		theirs := nearFile(t, "neartheirsk", ben, "", base, 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, anna, map[string]bool{mine.Id: true})), 0)
		// Keeping one file says nothing about another
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, anna, map[string]bool{"other": true})), 1)
	})

	t.Run("exact SHA1 twins are not near-copies", func(t *testing.T) {
		mine := nearFile(t, "neartwina", anna, "sha-neartwin", base, 0)
		theirs := nearFile(t, "neartwinb", ben, "sha-neartwin", base, 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, anna, nil)), 0)
	})

	t.Run("a zero PHash is never compared", func(t *testing.T) {
		mine := nearFile(t, "nearzeroa", anna, "", 0, 0)
		theirs := nearFile(t, "nearzerob", ben, "", 0b11, 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, anna, nil)), 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, ben, nil)), 0)
	})

	t.Run("a preview that is not ready is never compared", func(t *testing.T) {
		mine := nearFile(t, "nearpenda", anna, "", base, 0)
		theirs := nearFile(t, "nearpendb", ben, "", base^0b1, 0)
		database.SavePreview(models.Preview{Sha1: theirs.SHA1, Status: models.PreviewPending, PHash: base ^ 0b1})
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, anna, nil)), 0)
	})

	t.Run("no viewer, no flags", func(t *testing.T) {
		mine := nearFile(t, "nearanona", 0, "", base, 0)
		theirs := nearFile(t, "nearanonb", ben, "", base^0b1, 0)
		test.IsEqualInt(t, len(NearCopies([]models.File{mine, theirs}, 0, nil)), 0)
	})
}
