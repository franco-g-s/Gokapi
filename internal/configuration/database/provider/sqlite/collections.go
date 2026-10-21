package sqlite

import (
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

// Columns are listed explicitly rather than using SELECT *, so that a future
// ALTER TABLE cannot silently break the scan order.
const collectionColumns = `Id, Name, UserId, Creation, PasswordHash`

func (p DatabaseProvider) scanCollection(scan func(dest ...any) error) (models.Collection, error) {
	var c models.Collection
	err := scan(&c.Id, &c.Name, &c.UserId, &c.CreationDate, &c.PasswordHash)
	if err != nil {
		return models.Collection{}, err
	}
	return c, nil
}

// getCollectionFileIds returns the ordered member file IDs of a collection
func (p DatabaseProvider) getCollectionFileIds(collectionId string) []string {
	result := make([]string, 0)
	rows, err := p.sqliteDb.Query("SELECT FileId FROM CollectionFiles WHERE CollectionId = ? ORDER BY Position ASC", collectionId)
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		var fileId string
		err = rows.Scan(&fileId)
		helper.Check(err)
		result = append(result, fileId)
	}
	return result
}

// GetCollection returns the Collection or false if not found
func (p DatabaseProvider) GetCollection(id string) (models.Collection, bool) {
	if id == "" {
		return models.Collection{}, false
	}
	row := p.sqliteDb.QueryRow("SELECT "+collectionColumns+" FROM Collections WHERE Id = ?", id)
	result, err := p.scanCollection(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Collection{}, false
		}
		helper.Check(err)
		return models.Collection{}, false
	}
	result.FileIds = p.getCollectionFileIds(result.Id)
	return result, true
}

// GetAllCollections returns all collections, newest first
func (p DatabaseProvider) GetAllCollections() []models.Collection {
	result := make([]models.Collection, 0)
	rows, err := p.sqliteDb.Query("SELECT " + collectionColumns + " FROM Collections ORDER BY Creation DESC, Name")
	helper.Check(err)
	defer rows.Close()
	for rows.Next() {
		collection, err := p.scanCollection(rows.Scan)
		helper.Check(err)
		result = append(result, collection)
	}
	// Member IDs are fetched after the rows cursor is closed, as SQLite does not
	// allow a second query while iterating the first.
	for i := range result {
		result[i].FileIds = p.getCollectionFileIds(result[i].Id)
	}
	return result
}

// SaveCollection stores the collection and replaces its member list
func (p DatabaseProvider) SaveCollection(collection models.Collection) {
	_, err := p.sqliteDb.Exec(`INSERT OR REPLACE INTO Collections
		(Id, Name, UserId, Creation, PasswordHash) VALUES (?, ?, ?, ?, ?)`,
		collection.Id, collection.Name, collection.UserId, collection.CreationDate, collection.PasswordHash)
	helper.Check(err)

	_, err = p.sqliteDb.Exec("DELETE FROM CollectionFiles WHERE CollectionId = ?", collection.Id)
	helper.Check(err)
	for position, fileId := range collection.FileIds {
		_, err = p.sqliteDb.Exec("INSERT OR REPLACE INTO CollectionFiles (CollectionId, FileId, Position) VALUES (?, ?, ?)",
			collection.Id, fileId, position)
		helper.Check(err)
	}
}

// DeleteCollection deletes the collection and its member links. The files
// themselves are never touched.
func (p DatabaseProvider) DeleteCollection(id string) {
	if id == "" {
		return
	}
	_, err := p.sqliteDb.Exec("DELETE FROM CollectionFiles WHERE CollectionId = ?", id)
	helper.Check(err)
	_, err = p.sqliteDb.Exec("DELETE FROM CollectionsSeen WHERE CollectionId = ?", id)
	helper.Check(err)
	// Part 2, R4: the link of a collection turned into a group goes with it
	_, err = p.sqliteDb.Exec(`DELETE FROM "CollectionGroups" WHERE CollectionId = ?`, id)
	helper.Check(err)
	_, err = p.sqliteDb.Exec("DELETE FROM Collections WHERE Id = ?", id)
	helper.Check(err)
}

// RemoveFileFromCollections drops a file from every collection it belongs to.
// Called when a file is deleted, so collections never reference dead files.
func (p DatabaseProvider) RemoveFileFromCollections(fileId string) {
	if fileId == "" {
		return
	}
	_, err := p.sqliteDb.Exec("DELETE FROM CollectionFiles WHERE FileId = ?", fileId)
	helper.Check(err)
}
