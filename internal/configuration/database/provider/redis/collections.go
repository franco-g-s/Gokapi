package redis

import (
	"cmp"
	"slices"
	"strings"

	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	prefixCollections = "col:"
	// Member IDs are a slice, which cannot live in a Redis hash field directly,
	// so they are persisted as a single delimited string alongside the struct.
	fieldCollectionFiles = "fileids"
	collectionFilesSep   = ","
)

func dbToCollection(input []any) (models.Collection, error) {
	var result models.Collection
	err := redigo.ScanStruct(input, &result)
	if err != nil {
		return models.Collection{}, err
	}
	result.FileIds = splitFileIds(extractRawField(input, fieldCollectionFiles))
	return result, nil
}

// extractRawField pulls a single field out of a raw hash reply. ScanStruct
// ignores fields with no matching tag, so this is read separately.
func extractRawField(input []any, field string) string {
	for i := 0; i+1 < len(input); i += 2 {
		key, err := redigo.String(input[i], nil)
		if err != nil || key != field {
			continue
		}
		value, err := redigo.String(input[i+1], nil)
		if err != nil {
			return ""
		}
		return value
	}
	return ""
}

func splitFileIds(joined string) []string {
	result := make([]string, 0)
	for _, id := range strings.Split(joined, collectionFilesSep) {
		id = strings.TrimSpace(id)
		if id != "" {
			result = append(result, id)
		}
	}
	return result
}

// GetCollection returns the Collection or false if not found
func (p DatabaseProvider) GetCollection(id string) (models.Collection, bool) {
	if id == "" {
		return models.Collection{}, false
	}
	result, ok := p.getHashMap(prefixCollections + id)
	if !ok {
		return models.Collection{}, false
	}
	collection, err := dbToCollection(result)
	helper.Check(err)
	return collection, true
}

// GetAllCollections returns all collections, newest first
func (p DatabaseProvider) GetAllCollections() []models.Collection {
	result := make([]models.Collection, 0)
	maps := p.getAllHashesWithPrefix(prefixCollections)
	for _, v := range maps {
		collection, err := dbToCollection(v)
		helper.Check(err)
		result = append(result, collection)
	}
	return sortCollections(result)
}

func sortCollections(collections []models.Collection) []models.Collection {
	slices.SortFunc(collections, func(a, b models.Collection) int {
		return cmp.Or(
			cmp.Compare(b.CreationDate, a.CreationDate),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return collections
}

// SaveCollection stores the collection and its member list
func (p DatabaseProvider) SaveCollection(collection models.Collection) {
	args := p.buildArgs(prefixCollections + collection.Id).AddFlat(collection)
	args = args.Add(fieldCollectionFiles, strings.Join(collection.FileIds, collectionFilesSep))
	p.setHashMap(args)
}

// DeleteCollection deletes the collection. The files themselves are untouched.
func (p DatabaseProvider) DeleteCollection(id string) {
	if id == "" {
		return
	}
	p.deleteKey(prefixCollections + id)
}

// RemoveFileFromCollections drops a file from every collection it belongs to
func (p DatabaseProvider) RemoveFileFromCollections(fileId string) {
	if fileId == "" {
		return
	}
	for _, collection := range p.GetAllCollections() {
		filtered := make([]string, 0, len(collection.FileIds))
		for _, id := range collection.FileIds {
			if id != fileId {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) != len(collection.FileIds) {
			collection.FileIds = filtered
			p.SaveCollection(collection)
		}
	}
}
