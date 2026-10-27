package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

const previewColumns = `Sha1, Status, Kind, Width, Height, TakenAt, DurationMs, Color, PHash, Attempts, Updated`

func scanPreview(scan func(dest ...any) error) (models.Preview, error) {
	var p models.Preview
	var phash int64
	err := scan(&p.Sha1, &p.Status, &p.Kind, &p.Width, &p.Height, &p.TakenAt, &p.DurationMs, &p.Color, &phash, &p.Attempts, &p.Updated)
	p.PHash = uint64(phash)
	return p, err
}

// GetPreview returns the preview row for a blob hash
func (p DatabaseProvider) GetPreview(sha1 string) (models.Preview, bool) {
	row := p.sqliteDb.QueryRow("SELECT "+previewColumns+" FROM Previews WHERE Sha1 = ?", sha1)
	result, err := scanPreview(row.Scan)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			helper.Check(err)
		}
		return models.Preview{}, false
	}
	return result, true
}

// SavePreview inserts or replaces a preview row
func (p DatabaseProvider) SavePreview(pr models.Preview) {
	_, err := p.sqliteDb.Exec(`INSERT OR REPLACE INTO Previews (`+previewColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pr.Sha1, pr.Status, pr.Kind, pr.Width, pr.Height, pr.TakenAt, pr.DurationMs, pr.Color, int64(pr.PHash), pr.Attempts, pr.Updated)
	helper.Check(err)
}

// DeletePreview removes a preview row
func (p DatabaseProvider) DeletePreview(sha1 string) {
	_, err := p.sqliteDb.Exec("DELETE FROM Previews WHERE Sha1 = ?", sha1)
	helper.Check(err)
}

// GetAllPreviewSha1s maps every known blob hash to its preview status
func (p DatabaseProvider) GetAllPreviewSha1s() map[string]string {
	result := make(map[string]string)
	rows, err := p.sqliteDb.Query("SELECT Sha1, Status FROM Previews")
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var sha, status string
		helper.Check(rows.Scan(&sha, &status))
		result[sha] = status
	}
	return result
}
