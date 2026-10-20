package webserver

import (
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
)

// sharedCollectionInfo is one row of the "Shared with me" list on the
// Collections tab: a collection somebody else made that this user has opened.
type sharedCollectionInfo struct {
	Id          string
	Name        string
	Url         string
	OwnerName   string
	FileCount   int
	TotalSize   string
	FirstOpened string
}

// sharedWithMe lists the collections the user has opened that belong to
// someone else, newest first. Collections that are gone, or whose files have
// all expired, are skipped: GetCollection resolves live collections only.
func sharedWithMe(user models.User, serverUrl string) []sharedCollectionInfo {
	rows := make([]sharedCollectionInfo, 0)
	for _, seen := range database.GetCollectionsSeen(user.Id) {
		collection, ok := storage.GetCollection(seen.CollectionId)
		if !ok || collection.UserId == user.Id {
			continue
		}
		owner := "Unknown"
		if u, found := database.GetUser(collection.UserId); found {
			owner = displayName(u)
		}
		rows = append(rows, sharedCollectionInfo{
			Id:          collection.Id,
			Name:        collection.Name,
			Url:         serverUrl + "c?id=" + collection.Id,
			OwnerName:   owner,
			FileCount:   len(collection.Files),
			TotalSize:   helper.ByteCountSI(collection.TotalSizeBytes),
			FirstOpened: time.Unix(seen.FirstOpened, 0).Format("2006-01-02 15:04"),
		})
	}
	return rows
}
