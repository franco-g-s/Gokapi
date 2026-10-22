package webserver

import (
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/storage"
)

// userFileStat is what the Users page shows of one person's files
type userFileStat struct {
	Count int
	Bytes int64
}

// userFileStats counts, per uploader, the files the Files tab lists: the same
// rule as isListedFile, so the count on the Users page is the number of tiles
// the Files tab shows for them (/files?user=<id>).
func userFileStats() map[int]userFileStat {
	config := configuration.Get()
	now := time.Now().Unix()
	result := make(map[int]userFileStat)
	for _, f := range database.GetAllMetadata() {
		out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
		helper.Check(err)
		if !isListedFile(out, now) {
			continue
		}
		stat := result[f.UserId]
		stat.Count++
		stat.Bytes += f.SizeBytes
		result[f.UserId] = stat
	}
	return result
}

// userGroupNames is the names of the live groups the user belongs to
func userGroupNames(userId int) []string {
	groups := storage.GroupsOfUser(userId)
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}
