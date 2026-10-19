//go:build !integration && test

package webserver

import (
	"net/http"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// /cz?files= narrows a collection zip to some of its members, and can never
// add a file that is not a member.
func TestCollectionZipFilesFilter(t *testing.T) {
	a := accessFile("czfiltera00001", func(f *models.File) { f.SHA1 = "czfilterasha"; f.Name = "a.jpg" })
	b := accessFile("czfilterb00001", func(f *models.File) { f.SHA1 = "czfilterbsha"; f.Name = "b.jpg" })
	outsider := accessFile("czfilterout001", func(f *models.File) {
		f.SHA1 = "czfilteroutsha"
		f.Name = "outsider.jpg"
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 5
	})
	collection, err := storage.CreateCollection(storage.CollectionParameters{FileIds: []string{a.Id, b.Id}}, models.User{Id: 7})
	test.IsNil(t, err)
	zipGet := func(query string) (int, []string) {
		code, body := filesGet(t, "", "/cz?id="+collection.Id+query)
		if code != http.StatusOK {
			return code, nil
		}
		return code, zipEntries(t, body)
	}
	// No filter: every member
	code, names := zipGet("")
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualInt(t, len(names), 2)
	// One member
	code, names = zipGet("&files=" + b.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, strings.Join(names, ","), "b.jpg")
	// A non-member is ignored, never added
	code, names = zipGet("&files=" + a.Id + "," + outsider.Id)
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualString(t, strings.Join(names, ","), "a.jpg")
	// Only non-members, or an empty filter: nothing, not everything
	code, _ = zipGet("&files=" + outsider.Id)
	test.IsEqualInt(t, code, http.StatusGone)
	code, _ = zipGet("&files=")
	test.IsEqualInt(t, code, http.StatusGone)
	remaining, count := downloadState(t, outsider.Id)
	test.IsEqualInt(t, remaining, 5)
	test.IsEqualInt(t, count, 0)
}
