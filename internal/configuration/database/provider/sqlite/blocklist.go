package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

// ensureBlocklistTable creates the blocklist table if missing (Part 4, K).
// Like the group tables it runs on every start, so the schema version stays 17.
func (p DatabaseProvider) ensureBlocklistTable() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Blocklist" (
			"Entry"	TEXT NOT NULL PRIMARY KEY,
			"Added"	INTEGER NOT NULL,
			"AddedBy"	INTEGER NOT NULL
		);`)
}

// SaveBlock inserts a blocklist entry; an existing entry is left as it is
func (p DatabaseProvider) SaveBlock(b models.BlockEntry) {
	_, err := p.sqliteDb.Exec(`INSERT OR IGNORE INTO "Blocklist" (Entry, Added, AddedBy) VALUES (?, ?, ?)`, b.Entry, b.Added, b.AddedBy)
	helper.Check(err)
}

// DeleteBlock removes a blocklist entry
func (p DatabaseProvider) DeleteBlock(entry string) {
	_, err := p.sqliteDb.Exec(`DELETE FROM "Blocklist" WHERE Entry = ?`, entry)
	helper.Check(err)
}

// IsBlocked returns true if the exact entry is on the blocklist
func (p DatabaseProvider) IsBlocked(entry string) bool {
	var found int
	err := p.sqliteDb.QueryRow(`SELECT 1 FROM "Blocklist" WHERE Entry = ?`, entry).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	// any other error panics: a blocklist that cannot be read must not say "not blocked"
	helper.Check(err)
	return true
}

// GetAllBlocks returns every blocklist entry, oldest first
func (p DatabaseProvider) GetAllBlocks() []models.BlockEntry {
	result := make([]models.BlockEntry, 0)
	rows, err := p.sqliteDb.Query(`SELECT Entry, Added, AddedBy FROM "Blocklist" ORDER BY Added, Entry`)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var b models.BlockEntry
		helper.Check(rows.Scan(&b.Entry, &b.Added, &b.AddedBy))
		result = append(result, b)
	}
	return result
}
