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
	// The switch, and anna's file once, through a group's counted download
	test.IsEqualBool(t, strings.Contains(body, `data-scope="all"`), true)
	test.IsEqualBool(t, strings.Contains(body, `data-scope="mine"`), true)
	test.IsEqualInt(t, strings.Count(body, `<figure class="fgs-tile" data-mine="false" data-id="`+annas.Id+`"`), 1)
	tile := tileOf(t, body, annas.Id)
	test.IsEqualBool(t, strings.Contains(tile, `data-foreign="true"`), true)
	test.IsEqualBool(t, strings.Contains(tile, `data-uploader="anna"`), true)
	test.IsEqualBool(t, strings.Contains(tile, `data-download="./gd?id=`), true)
	test.IsEqualBool(t, strings.Contains(tile, `&amp;file=`+annas.Id+`"`), true)
	test.IsEqualBool(t, strings.Contains(body, "anna@example.com"), false)
	// Not editable, and none of its management data
	menu := menuOf(t, body, annas.Id)
	for _, leak := range []string{"data-url", "data-downloads", "data-can-edit"} {
		test.IsEqualBool(t, strings.Contains(menu, leak), false)
	}
	test.IsEqualBool(t, strings.Contains(menu, `data-can-delete="false"`), true)
	test.IsEqualBool(t, strings.Contains(body, "/d?id="+annas.Id), false)
	test.IsEqualBool(t, strings.Contains(body, `id="row-`+annas.Id+`"`), false)
	// Files anna keeps to herself stay hers
	test.IsEqualBool(t, strings.Contains(body, private.Id), false)
	// ben's own file is listed as his, with its management data
	test.IsEqualInt(t, strings.Count(body, `data-mine="true" data-id="`+bens.Id+`"`), 1)
	test.IsEqualBool(t, strings.Contains(menuOf(t, body, bens.Id), `data-url="`), true)

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

	// anna, whose groups hold only her own files and ben's: ben's file once
	body = groupPageCall(showFiles, "GET", "/files", anna, "").Body.String()
	test.IsEqualBool(t, strings.Contains(body, `data-scope="all"`), true)
	test.IsEqualInt(t, strings.Count(body, `data-mine="true" data-id="`+annas.Id+`"`), 1)
	test.IsEqualInt(t, strings.Count(body, `<figure class="fgs-tile" data-mine="false" data-id="`+bens.Id+`"`), 1)
}
