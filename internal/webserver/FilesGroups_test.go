//go:build !integration && test

package webserver

import (
	"net/http"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// The Files tab for someone without PERM_LIST (spec, "Files tab"): the
// My files / Everything switch appears once they are in a live group with
// other people's files, and Everything adds those files, each once, through
// the group's download, under the uploader's display name, not editable and
// without their management data.
func TestFilesTabEverythingFromGroups(t *testing.T) {
	anna := models.User{Id: 81, Name: "anna@example.com", UserLevel: models.UserLevelUser}
	ben := models.User{Id: 82, Name: "ben@example.com", UserLevel: models.UserLevelUser}
	carl := models.User{Id: 83, Name: "carl@example.com", UserLevel: models.UserLevelUser}
	for _, u := range []models.User{anna, ben, carl} {
		database.SaveUser(u, false)
	}
	one, err := storage.CreateGroup("Iceland", 30, anna)
	test.IsNil(t, err)
	two, err := storage.CreateGroup("Faroe", 30, anna)
	test.IsNil(t, err)
	for _, g := range []models.Group{one, two} {
		_, err = storage.JoinGroupByToken(g.InviteToken, ben)
		test.IsNil(t, err)
	}
	annas := accessFile("filesgroupanna00001", func(f *models.File) { f.UserId = anna.Id; f.SHA1 = "filesgroupannasha"; f.Name = "fjord.jpg" })
	private := accessFile("filesgroupanna00002", func(f *models.File) { f.UserId = anna.Id; f.SHA1 = "filesgroupannasha2"; f.Name = "private.jpg" })
	bens := accessFile("filesgroupben000001", func(f *models.File) { f.UserId = ben.Id; f.SHA1 = "filesgroupbensha"; f.Name = "glacier.jpg" })
	t.Cleanup(func() {
		database.DeleteGroup(one.Id)
		database.DeleteGroup(two.Id)
		for _, f := range []models.File{annas, private, bens} {
			database.RemoveFileFromGroups(f.Id)
			database.DeleteMetaData(f.Id)
		}
		for _, u := range []models.User{anna, ben, carl} {
			database.DeleteUser(u.Id)
		}
	})
	// anna's file is in both groups, ben's in one
	for _, g := range []models.Group{one, two} {
		added, err := storage.AddFilesToGroup(g, []string{annas.Id}, anna)
		test.IsNil(t, err)
		test.IsEqualInt(t, len(added.Added), 1)
	}
	added, err := storage.AddFilesToGroup(one, []string{bens.Id}, ben)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(added.Added), 1)

	w := groupPageCall(showFiles, "GET", "/files", ben, "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	body := w.Body.String()
	// The switch; the page renders My files, its default (Part 3)
	test.IsEqualBool(t, strings.Contains(body, `data-scope="all"`), true)
	test.IsEqualBool(t, strings.Contains(body, `data-scope="mine"`), true)
	test.IsEqualBool(t, strings.Contains(body, annas.Id), false)
	test.IsEqualBool(t, strings.Contains(body, "anna@example.com"), false)
	// ben's own file is listed as his, with its management data
	test.IsEqualInt(t, strings.Count(body, `data-mine="true" data-id="`+bens.Id+`"`), 1)
	test.IsEqualBool(t, strings.Contains(menuOf(t, body, bens.Id), `data-url="`), true)

	// Everything (the feed): anna's file once, through a group's counted
	// download, not editable and without its management data
	everything := decodeList(t, groupPageCall(fileListFeed, "GET", "/files/list?src=files&scope=all", ben, ""))
	raw := groupPageCall(fileListFeed, "GET", "/files/list?src=files&scope=all", ben, "").Body.String()
	count := 0
	for _, item := range everything.Items {
		switch item.Id {
		case annas.Id:
			count++
			test.IsEqualBool(t, item.Foreign, true)
			test.IsEqualBool(t, item.Mine, false)
			test.IsEqualString(t, item.UploaderName, "anna")
			test.IsEqualBool(t, strings.HasPrefix(item.Download, "./gd?id="), true)
			test.IsEqualBool(t, strings.HasSuffix(item.Download, "&file="+annas.Id), true)
			test.IsEqualString(t, item.UrlDownload, "")
			test.IsEqualBool(t, item.CanEdit || item.CanDelete || item.CanReplace, false)
		case bens.Id:
			test.IsEqualBool(t, item.Mine, true)
			test.IsNotEmpty(t, item.UrlDownload)
		}
	}
	test.IsEqualInt(t, count, 1)
	test.IsEqualBool(t, strings.Contains(raw, "anna@example.com"), false)
	test.IsEqualBool(t, strings.Contains(raw, "/d?id="+annas.Id), false)
	// Files anna keeps to herself stay hers
	test.IsEqualBool(t, strings.Contains(raw, private.Id), false)

	// A pending preview of anna's file can be polled by ben, without a download
	w = groupPageCall(filePreviewInfo, "GET", "/files/previewinfo?id="+annas.Id, ben, "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), `"Download":""`), true)
	// but not by someone outside her groups
	w = groupPageCall(filePreviewInfo, "GET", "/files/previewinfo?id="+annas.Id, carl, "")
	test.IsEqualInt(t, w.Code, http.StatusNotFound)

	// Someone in no group gets no switch
	body = groupPageCall(showFiles, "GET", "/files", carl, "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, `data-scope="all"`), false)
	test.IsEqualBool(t, strings.Contains(body, annas.Id), false)

	// dora, in a live group holding only her own file: the switch, and
	// nothing of anyone else's
	dora := models.User{Id: 84, Name: "dora@example.com", UserLevel: models.UserLevelUser}
	database.SaveUser(dora, false)
	defer database.DeleteUser(dora.Id)
	solo, err := storage.CreateGroup("Solo", 30, dora)
	test.IsNil(t, err)
	defer database.DeleteGroup(solo.Id)
	doras := accessFile("filesgroupdora00001", func(f *models.File) { f.UserId = dora.Id; f.SHA1 = "filesgroupdorasha" })
	defer func() {
		database.RemoveFileFromGroups(doras.Id)
		database.DeleteMetaData(doras.Id)
	}()
	_, err = storage.AddFilesToGroup(solo, []string{doras.Id}, dora)
	test.IsNil(t, err)
	body = groupPageCall(showFiles, "GET", "/files", dora, "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, `data-scope="all"`), true)
	test.IsEqualBool(t, strings.Contains(body, `data-foreign="true"`), false)
	test.IsEqualInt(t, strings.Count(body, `data-mine="true" data-id="`+doras.Id+`"`), 1)

	// anna, whose groups hold only her own files and ben's: ben's file once
	body = groupPageCall(showFiles, "GET", "/files", anna, "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, `data-scope="all"`), true)
	test.IsEqualInt(t, strings.Count(body, `data-mine="true" data-id="`+annas.Id+`"`), 1)
	everything = decodeList(t, groupPageCall(fileListFeed, "GET", "/files/list?src=files&scope=all", anna, ""))
	count = 0
	for _, item := range everything.Items {
		if item.Id == bens.Id {
			count++
			test.IsEqualBool(t, item.Foreign, true)
		}
	}
	test.IsEqualInt(t, count, 1)
}
