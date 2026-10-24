package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

// ensureActivityTable creates the activity feed table if missing (Part 4, A).
// Like ensureGroupTables it runs on every start, so the schema version stays
// at upstream's 17. AUTOINCREMENT is deliberate: ids are never reused after
// pruning, so a feed cursor stays meaningful.
func (p DatabaseProvider) ensureActivityTable() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Activity" (
			"Id"	INTEGER PRIMARY KEY AUTOINCREMENT,
			"At"	INTEGER NOT NULL,
			"Kind"	TEXT NOT NULL,
			"ActorId"	INTEGER NOT NULL DEFAULT 0,
			"OwnerId"	INTEGER NOT NULL DEFAULT 0,
			"FileId"	TEXT NOT NULL DEFAULT '',
			"FileName"	TEXT NOT NULL DEFAULT '',
			"FileSize"	INTEGER NOT NULL DEFAULT 0,
			"FileKind"	TEXT NOT NULL DEFAULT '',
			"GroupId"	TEXT NOT NULL DEFAULT '',
			"GroupName"	TEXT NOT NULL DEFAULT '',
			"Via"	TEXT NOT NULL DEFAULT '',
			"Detail"	TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS "idx_Activity_At" ON "Activity" ("At");`)
}

const activityColumns = `At, Kind, ActorId, OwnerId, FileId, FileName, FileSize, FileKind, GroupId, GroupName, Via, Detail`

// SaveActivity appends one event; its Id is assigned by the database
func (p DatabaseProvider) SaveActivity(a models.Activity) {
	_, err := p.sqliteDb.Exec(`INSERT INTO "Activity" (`+activityColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.At, a.Kind, a.ActorId, a.OwnerId, a.FileId, a.FileName, a.FileSize, a.FileKind, a.GroupId, a.GroupName, a.Via, a.Detail)
	helper.Check(err)
}

// GetActivitySince returns up to limit events with an Id above since, oldest first
func (p DatabaseProvider) GetActivitySince(since int64, limit int) []models.Activity {
	result := make([]models.Activity, 0)
	rows, err := p.sqliteDb.Query(`SELECT Id, `+activityColumns+` FROM "Activity" WHERE Id > ? ORDER BY Id LIMIT ?`, since, limit)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var a models.Activity
		helper.Check(rows.Scan(&a.Id, &a.At, &a.Kind, &a.ActorId, &a.OwnerId, &a.FileId, &a.FileName, &a.FileSize,
			&a.FileKind, &a.GroupId, &a.GroupName, &a.Via, &a.Detail))
		result = append(result, a)
	}
	return result
}

// ActivityBounds returns the oldest retained id (0 when the table is empty)
// and the newest id ever assigned, which survives pruning (0 if none ever)
func (p DatabaseProvider) ActivityBounds() (oldest, newest int64) {
	var minId sql.NullInt64
	helper.Check(p.sqliteDb.QueryRow(`SELECT MIN(Id) FROM "Activity"`).Scan(&minId))
	err := p.sqliteDb.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'Activity'`).Scan(&newest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		helper.Check(err)
	}
	return minId.Int64, newest
}

// PruneActivity deletes every event that happened before the timestamp
func (p DatabaseProvider) PruneActivity(before int64) {
	_, err := p.sqliteDb.Exec(`DELETE FROM "Activity" WHERE At < ?`, before)
	helper.Check(err)
}
