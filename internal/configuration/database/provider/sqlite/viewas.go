package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

// ensureViewAsTable creates the "View as…" table if missing (Part 3, V1).
// Like ensureGroupTables it runs on every start, so the schema version stays
// at upstream's 17 and rollback stays an image swap.
func (p DatabaseProvider) ensureViewAsTable() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "ViewAs" (
			"AdminId"	INTEGER PRIMARY KEY,
			"TargetId"	INTEGER NOT NULL,
			"Started"	INTEGER NOT NULL,
			"LastSeen"	INTEGER NOT NULL
		);`)
}

const viewAsColumns = `AdminId, TargetId, Started, LastSeen`

// GetViewAs returns the admin's active "View as…", or false
func (p DatabaseProvider) GetViewAs(adminId int) (models.ViewAs, bool) {
	var v models.ViewAs
	err := p.sqliteDb.QueryRow(`SELECT `+viewAsColumns+` FROM "ViewAs" WHERE AdminId = ?`, adminId).
		Scan(&v.AdminId, &v.TargetId, &v.Started, &v.LastSeen)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			helper.Check(err)
		}
		return models.ViewAs{}, false
	}
	return v, true
}

// GetAllViewAs returns every active "View as…"
func (p DatabaseProvider) GetAllViewAs() []models.ViewAs {
	result := make([]models.ViewAs, 0)
	rows, err := p.sqliteDb.Query(`SELECT ` + viewAsColumns + ` FROM "ViewAs" ORDER BY AdminId`)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var v models.ViewAs
		helper.Check(rows.Scan(&v.AdminId, &v.TargetId, &v.Started, &v.LastSeen))
		result = append(result, v)
	}
	return result
}

// SaveViewAs inserts or replaces the admin's "View as…"
func (p DatabaseProvider) SaveViewAs(v models.ViewAs) {
	_, err := p.sqliteDb.Exec(`INSERT OR REPLACE INTO "ViewAs" (`+viewAsColumns+`) VALUES (?, ?, ?, ?)`,
		v.AdminId, v.TargetId, v.Started, v.LastSeen)
	helper.Check(err)
}

// DeleteViewAs ends the admin's "View as…"
func (p DatabaseProvider) DeleteViewAs(adminId int) {
	_, err := p.sqliteDb.Exec(`DELETE FROM "ViewAs" WHERE AdminId = ?`, adminId)
	helper.Check(err)
}
