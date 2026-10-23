package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

// ensureProfilesTable creates the profiles table if missing (Part 4, S1).
// Like the other fork tables it runs on every start: the schema version stays
// at upstream's 17 and rollback stays an image swap.
func (p DatabaseProvider) ensureProfilesTable() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Profiles" (
			"UserId"	INTEGER PRIMARY KEY,
			"DisplayName"	TEXT NOT NULL DEFAULT '',
			"GoogleName"	TEXT NOT NULL DEFAULT '',
			"Photo"	BLOB,
			"PhotoType"	TEXT NOT NULL DEFAULT '',
			"PhotoUpdated"	INTEGER NOT NULL DEFAULT 0,
			"UseInitials"	INTEGER NOT NULL DEFAULT 0,
			"EmailDownloads"	INTEGER NOT NULL DEFAULT 0
		);`)
}

const profileColumns = `UserId, DisplayName, GoogleName, Photo, PhotoType, PhotoUpdated, UseInitials, EmailDownloads`

// GetProfile returns the user's profile, or false if none is stored
func (p DatabaseProvider) GetProfile(userId int) (models.Profile, bool) {
	var result models.Profile
	var useInitials, emailDownloads int
	err := p.sqliteDb.QueryRow(`SELECT `+profileColumns+` FROM "Profiles" WHERE UserId = ?`, userId).
		Scan(&result.UserId, &result.DisplayName, &result.GoogleName, &result.Photo, &result.PhotoType,
			&result.PhotoUpdated, &useInitials, &emailDownloads)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			helper.Check(err)
		}
		return models.Profile{}, false
	}
	result.UseInitials = useInitials == 1
	result.EmailDownloads = emailDownloads == 1
	return result, true
}

// GetProfileWithoutPhoto is GetProfile without reading the photo's bytes:
// names and photo URLs are looked up once per row of every list
func (p DatabaseProvider) GetProfileWithoutPhoto(userId int) (models.Profile, bool) {
	var result models.Profile
	var useInitials, emailDownloads, photoSize int
	err := p.sqliteDb.QueryRow(`SELECT UserId, DisplayName, GoogleName, IFNULL(LENGTH(Photo), 0), PhotoType,
		PhotoUpdated, UseInitials, EmailDownloads FROM "Profiles" WHERE UserId = ?`, userId).
		Scan(&result.UserId, &result.DisplayName, &result.GoogleName, &photoSize, &result.PhotoType,
			&result.PhotoUpdated, &useInitials, &emailDownloads)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			helper.Check(err)
		}
		return models.Profile{}, false
	}
	if photoSize == 0 {
		result.PhotoType = ""
	}
	result.UseInitials = useInitials == 1
	result.EmailDownloads = emailDownloads == 1
	return result, true
}

// SaveProfile inserts or replaces the user's profile
func (p DatabaseProvider) SaveProfile(profile models.Profile) {
	useInitials, emailDownloads := 0, 0
	if profile.UseInitials {
		useInitials = 1
	}
	if profile.EmailDownloads {
		emailDownloads = 1
	}
	_, err := p.sqliteDb.Exec(`INSERT OR REPLACE INTO "Profiles" (`+profileColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.UserId, profile.DisplayName, profile.GoogleName, profile.Photo, profile.PhotoType,
		profile.PhotoUpdated, useInitials, emailDownloads)
	helper.Check(err)
}

// DeleteProfile removes the user's profile
func (p DatabaseProvider) DeleteProfile(userId int) {
	_, err := p.sqliteDb.Exec(`DELETE FROM "Profiles" WHERE UserId = ?`, userId)
	helper.Check(err)
}
