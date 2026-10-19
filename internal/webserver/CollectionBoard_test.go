//go:build test

package webserver

import (
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

func TestCollectionBoardMarkup(t *testing.T) {
	f := accessFile("boardfile0000000001", func(f *models.File) { f.SHA1 = "boardsha1"; f.Name = "IMG.JPG" })
	database.SavePreview(models.Preview{Sha1: "boardsha1", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Color: "#808080", Updated: 1})
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "b", FileIds: []string{f.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		RequiredContent: []string{`data-layout-default="list"`, "fgs-board", "./p/thumb?id=" + f.Id, "&amp;c=" + col.Id, "fgs-viewer",
			`id="fgs-collection-list"`, "fgs-open-viewer", "data-fgs-photos", `data-bytes="1"`, `data-bytes="1">`},
	})
}
