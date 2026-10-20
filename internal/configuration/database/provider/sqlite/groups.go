package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

// ensureGroupTables creates the fork's group, download and "shared with me"
// tables if missing. Runs on every start, like ensurePreviewsTable, so the
// schema version stays at upstream's 17 and rollback stays an image swap.
// "Groups" is always quoted: GROUPS is an SQLite keyword.
func (p DatabaseProvider) ensureGroupTables() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Groups" (
			"Id"	TEXT NOT NULL UNIQUE,
			"Name"	TEXT NOT NULL,
			"CreatorId"	INTEGER NOT NULL,
			"Created"	INTEGER NOT NULL,
			"Expires"	INTEGER NOT NULL,
			"JoinCode"	TEXT NOT NULL UNIQUE,
			"InviteToken"	TEXT NOT NULL UNIQUE,
			PRIMARY KEY("Id")
		) WITHOUT ROWID;
		CREATE TABLE IF NOT EXISTS "GroupMembers" (
			"GroupId"	TEXT NOT NULL,
			"UserId"	INTEGER NOT NULL,
			"Joined"	INTEGER NOT NULL,
			"Role"	TEXT NOT NULL,
			PRIMARY KEY("GroupId","UserId")
		) WITHOUT ROWID;
		CREATE INDEX IF NOT EXISTS "GroupMembersByUser" ON "GroupMembers" ("UserId");
		CREATE TABLE IF NOT EXISTS "GroupFiles" (
			"GroupId"	TEXT NOT NULL,
			"FileId"	TEXT NOT NULL,
			PRIMARY KEY("GroupId","FileId")
		) WITHOUT ROWID;
		CREATE INDEX IF NOT EXISTS "GroupFilesByFile" ON "GroupFiles" ("FileId");
		CREATE TABLE IF NOT EXISTS "GroupNearKept" (
			"GroupId"	TEXT NOT NULL,
			"FileId"	TEXT NOT NULL,
			PRIMARY KEY("GroupId","FileId")
		) WITHOUT ROWID;
		CREATE TABLE IF NOT EXISTS "Downloads" (
			"Id"	INTEGER PRIMARY KEY AUTOINCREMENT,
			"FileId"	TEXT NOT NULL,
			"UserId"	INTEGER,
			"At"	INTEGER NOT NULL,
			"Via"	TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS "DownloadsByUser" ON "Downloads" ("UserId","FileId");
		CREATE INDEX IF NOT EXISTS "DownloadsByFile" ON "Downloads" ("FileId");
		CREATE TABLE IF NOT EXISTS "CollectionsSeen" (
			"UserId"	INTEGER NOT NULL,
			"CollectionId"	TEXT NOT NULL,
			"FirstOpened"	INTEGER NOT NULL,
			PRIMARY KEY("UserId","CollectionId")
		) WITHOUT ROWID;`)
}

// Columns are listed explicitly rather than using SELECT *, as in collections.go
const groupColumns = `Id, Name, CreatorId, Created, Expires, JoinCode, InviteToken`

func scanGroup(scan func(dest ...any) error) (models.Group, error) {
	var g models.Group
	err := scan(&g.Id, &g.Name, &g.CreatorId, &g.Created, &g.Expires, &g.JoinCode, &g.InviteToken)
	if err != nil {
		return models.Group{}, err
	}
	return g, nil
}

// SupportsGroups is true: this provider has the group tables
func (p DatabaseProvider) SupportsGroups() bool {
	return true
}

func (p DatabaseProvider) getGroupWhere(column, value string) (models.Group, bool) {
	if value == "" {
		return models.Group{}, false
	}
	row := p.sqliteDb.QueryRow(`SELECT `+groupColumns+` FROM "Groups" WHERE `+column+` = ?`, value)
	result, err := scanGroup(row.Scan)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			helper.Check(err)
		}
		return models.Group{}, false
	}
	return result, true
}

// GetGroup returns the group or false if not found
func (p DatabaseProvider) GetGroup(id string) (models.Group, bool) {
	return p.getGroupWhere("Id", id)
}

// GetGroupByToken returns the group with this invite token
func (p DatabaseProvider) GetGroupByToken(token string) (models.Group, bool) {
	return p.getGroupWhere("InviteToken", token)
}

// GetGroupByCode returns the group with this join code, as stored
func (p DatabaseProvider) GetGroupByCode(code string) (models.Group, bool) {
	return p.getGroupWhere("JoinCode", code)
}

// GetAllGroups returns all groups, newest first
func (p DatabaseProvider) GetAllGroups() []models.Group {
	result := make([]models.Group, 0)
	rows, err := p.sqliteDb.Query(`SELECT ` + groupColumns + ` FROM "Groups" ORDER BY Created DESC, Name`)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		g, err := scanGroup(rows.Scan)
		helper.Check(err)
		result = append(result, g)
	}
	return result
}

// SaveGroup inserts or replaces a group
func (p DatabaseProvider) SaveGroup(g models.Group) {
	_, err := p.sqliteDb.Exec(`INSERT OR REPLACE INTO "Groups" (`+groupColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.Id, g.Name, g.CreatorId, g.Created, g.Expires, g.JoinCode, g.InviteToken)
	helper.Check(err)
}

// DeleteGroup removes the group, its members, its file links and its kept
// near-copies. Files are never touched.
func (p DatabaseProvider) DeleteGroup(id string) {
	if id == "" {
		return
	}
	for _, table := range []string{"GroupMembers", "GroupFiles", "GroupNearKept"} {
		_, err := p.sqliteDb.Exec(`DELETE FROM "`+table+`" WHERE GroupId = ?`, id)
		helper.Check(err)
	}
	_, err := p.sqliteDb.Exec(`DELETE FROM "Groups" WHERE Id = ?`, id)
	helper.Check(err)
}

func (p DatabaseProvider) queryGroupMembers(query string, arg any) []models.GroupMember {
	result := make([]models.GroupMember, 0)
	rows, err := p.sqliteDb.Query(query, arg)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var m models.GroupMember
		helper.Check(rows.Scan(&m.GroupId, &m.UserId, &m.Joined, &m.Role))
		result = append(result, m)
	}
	return result
}

// GetGroupMembers returns the members of a group, by Joined ascending
func (p DatabaseProvider) GetGroupMembers(groupId string) []models.GroupMember {
	return p.queryGroupMembers(`SELECT GroupId, UserId, Joined, Role FROM "GroupMembers"
		WHERE GroupId = ? ORDER BY Joined ASC, UserId ASC`, groupId)
}

// GetMembershipsOfUser returns every group membership of a user
func (p DatabaseProvider) GetMembershipsOfUser(userId int) []models.GroupMember {
	return p.queryGroupMembers(`SELECT GroupId, UserId, Joined, Role FROM "GroupMembers"
		WHERE UserId = ? ORDER BY Joined ASC, GroupId ASC`, userId)
}

// SaveGroupMember inserts or replaces a membership
func (p DatabaseProvider) SaveGroupMember(m models.GroupMember) {
	_, err := p.sqliteDb.Exec(`INSERT OR REPLACE INTO "GroupMembers" (GroupId, UserId, Joined, Role) VALUES (?, ?, ?, ?)`,
		m.GroupId, m.UserId, m.Joined, m.Role)
	helper.Check(err)
}

// DeleteGroupMember removes one membership
func (p DatabaseProvider) DeleteGroupMember(groupId string, userId int) {
	_, err := p.sqliteDb.Exec(`DELETE FROM "GroupMembers" WHERE GroupId = ? AND UserId = ?`, groupId, userId)
	helper.Check(err)
}

func (p DatabaseProvider) queryStrings(query string, arg any) []string {
	result := make([]string, 0)
	rows, err := p.sqliteDb.Query(query, arg)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var s string
		helper.Check(rows.Scan(&s))
		result = append(result, s)
	}
	return result
}

// GetGroupFileIds returns the ids of all files in a group
func (p DatabaseProvider) GetGroupFileIds(groupId string) []string {
	return p.queryStrings(`SELECT FileId FROM "GroupFiles" WHERE GroupId = ? ORDER BY FileId`, groupId)
}

// GetGroupIdsOfFile returns the ids of all groups a file is in
func (p DatabaseProvider) GetGroupIdsOfFile(fileId string) []string {
	return p.queryStrings(`SELECT GroupId FROM "GroupFiles" WHERE FileId = ? ORDER BY GroupId`, fileId)
}

// AddGroupFile puts a file into a group. Adding it twice changes nothing.
func (p DatabaseProvider) AddGroupFile(groupId, fileId string) {
	_, err := p.sqliteDb.Exec(`INSERT OR IGNORE INTO "GroupFiles" (GroupId, FileId) VALUES (?, ?)`, groupId, fileId)
	helper.Check(err)
}

// RemoveGroupFile takes a file out of one group
func (p DatabaseProvider) RemoveGroupFile(groupId, fileId string) {
	_, err := p.sqliteDb.Exec(`DELETE FROM "GroupFiles" WHERE GroupId = ? AND FileId = ?`, groupId, fileId)
	helper.Check(err)
}

// RemoveFileFromGroups drops a file from every group. Called when a file is
// deleted, so groups never reference dead files.
func (p DatabaseProvider) RemoveFileFromGroups(fileId string) {
	if fileId == "" {
		return
	}
	_, err := p.sqliteDb.Exec(`DELETE FROM "GroupFiles" WHERE FileId = ?`, fileId)
	helper.Check(err)
	_, err = p.sqliteDb.Exec(`DELETE FROM "GroupNearKept" WHERE FileId = ?`, fileId)
	helper.Check(err)
}

// SaveNearCopyKept remembers that "Keep both" was chosen for a file in a group
func (p DatabaseProvider) SaveNearCopyKept(groupId, fileId string) {
	_, err := p.sqliteDb.Exec(`INSERT OR IGNORE INTO "GroupNearKept" (GroupId, FileId) VALUES (?, ?)`, groupId, fileId)
	helper.Check(err)
}

// GetNearCopiesKept returns the file ids kept in a group despite looking like another's
func (p DatabaseProvider) GetNearCopiesKept(groupId string) map[string]bool {
	result := make(map[string]bool)
	for _, id := range p.queryStrings(`SELECT FileId FROM "GroupNearKept" WHERE GroupId = ?`, groupId) {
		result[id] = true
	}
	return result
}

// SaveDownload records one original file served. UserId 0 is stored as NULL.
func (p DatabaseProvider) SaveDownload(d models.Download) {
	_, err := p.sqliteDb.Exec(`INSERT INTO "Downloads" (FileId, UserId, At, Via) VALUES (?, ?, ?, ?)`,
		d.FileId, sql.NullInt64{Int64: int64(d.UserId), Valid: d.UserId != 0}, d.At, d.Via)
	helper.Check(err)
}

// GetDownloadedFileIds returns every file id the user has downloaded, by any route
func (p DatabaseProvider) GetDownloadedFileIds(userId int) map[string]bool {
	result := make(map[string]bool)
	if userId == 0 {
		return result
	}
	for _, id := range p.queryStrings(`SELECT DISTINCT FileId FROM "Downloads" WHERE UserId = ?`, userId) {
		result[id] = true
	}
	return result
}

// DeleteDownloadsOfFile removes the download rows of a file. Called when the file is deleted.
func (p DatabaseProvider) DeleteDownloadsOfFile(fileId string) {
	if fileId == "" {
		return
	}
	_, err := p.sqliteDb.Exec(`DELETE FROM "Downloads" WHERE FileId = ?`, fileId)
	helper.Check(err)
}

// SaveCollectionSeen records the first time a user opened a collection; later calls keep the first time
func (p DatabaseProvider) SaveCollectionSeen(s models.CollectionSeen) {
	_, err := p.sqliteDb.Exec(`INSERT OR IGNORE INTO "CollectionsSeen" (UserId, CollectionId, FirstOpened) VALUES (?, ?, ?)`,
		s.UserId, s.CollectionId, s.FirstOpened)
	helper.Check(err)
}

// GetCollectionsSeen returns the collections a user has opened, newest first
func (p DatabaseProvider) GetCollectionsSeen(userId int) []models.CollectionSeen {
	result := make([]models.CollectionSeen, 0)
	rows, err := p.sqliteDb.Query(`SELECT UserId, CollectionId, FirstOpened FROM "CollectionsSeen"
		WHERE UserId = ? ORDER BY FirstOpened DESC, CollectionId`, userId)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var s models.CollectionSeen
		helper.Check(rows.Scan(&s.UserId, &s.CollectionId, &s.FirstOpened))
		result = append(result, s)
	}
	return result
}
