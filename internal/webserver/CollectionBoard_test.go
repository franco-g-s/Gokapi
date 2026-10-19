//go:build test

package webserver

import (
	"os"
	"strings"
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
			`id="fgs-collection-list"`, "fgs-open-viewer", "fgs-collection-wide", "data-fgs-photos", `data-bytes="1"`, `data-bytes="1">`},
	})
}

// R5: a name in the Collections tab opens the collection in the same tab, the
// collection page widens on desktop, and the password page stays narrow.
func TestCollectionNamesLinkAndWidePage(t *testing.T) {
	f := accessFile("r5namefile0000000001", nil)
	col, err := storage.CreateCollection(storage.CollectionParameters{Name: "r5-linked-name", FileIds: []string{f.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	locked, err := storage.CreateCollection(storage.CollectionParameters{Name: "r5-locked", FileIds: []string{f.Id}, Password: "pw"}, models.User{Id: 7})
	test.IsNil(t, err)

	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/collections", IsHtml: true,
		Cookies:         []test.Cookie{filesAdminSession()},
		RequiredContent: []string{`<a class="collection-name-link" href="./c?id=` + col.Id + `">r5-linked-name</a>`},
	})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + col.Id, IsHtml: true,
		RequiredContent: []string{`class="file-card card fgs-collection-wide"`},
	})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/c?id=" + locked.Id, IsHtml: true,
		RequiredContent: []string{"This collection is password protected."},
		ExcludedContent: []string{"fgs-collection-wide"},
	})

	// Wide only from 992px up, so smaller screens keep the 450px card
	css, err := os.ReadFile("web/static/css/media.css")
	test.IsNil(t, err)
	rule := "@media (min-width:992px){.file-card.fgs-collection-wide{max-width:min(1280px,100% - 2rem)}}"
	test.IsEqualBool(t, strings.Contains(string(css), rule), true)
}
