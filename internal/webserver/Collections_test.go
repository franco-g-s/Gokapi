//go:build !integration && test

package webserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/access"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// Changing or removing a collection's password revokes every unlock cookie
// issued for the old one: /c, /cd and CanView all stop honouring it.
func TestCollectionPasswordChangeRevokesUnlocks(t *testing.T) {
	// A password-protected member, so CanView depends on the collection cookie
	// rather than letting any link holder through
	f := accessFile("colpwrevoke00000001", func(f *models.File) { f.PasswordHash = "x"; f.SHA1 = "colpwrevokesha" })
	owner := models.User{Id: 7}

	for _, newPassword := range []string{"changed", ""} {
		collection, err := storage.CreateCollection(storage.CollectionParameters{
			FileIds: []string{f.Id}, Password: "old"}, owner)
		test.IsNil(t, err)

		w := httptest.NewRecorder()
		writeCollectionPwCookie(w, collection)
		cookie := w.Result().Cookies()[0]
		withCookie := func(method, url string) *http.Request {
			r := httptest.NewRequest(method, url, nil)
			r.AddCookie(cookie)
			return r
		}
		boardUnlocked := func() bool {
			w := httptest.NewRecorder()
			showCollection(w, withCookie("GET", "/c?id="+collection.Id))
			return strings.Contains(w.Body.String(), "./cz?id="+collection.Id)
		}
		fileServed := func() bool {
			w := httptest.NewRecorder()
			downloadCollectionFile(w, withCookie("GET", "/cd?id="+collection.Id+"&file="+f.Id))
			return w.Code == http.StatusOK
		}
		canView := func() bool {
			return access.CanView(withCookie("GET", "/p/thumb"), f, collection.Id)
		}

		test.IsEqualBool(t, boardUnlocked(), true)
		test.IsEqualBool(t, fileServed(), true)
		test.IsEqualBool(t, canView(), true)

		body := `{"id":"` + collection.Id + `","password":"` + newPassword + `"}`
		r := httptest.NewRequest("POST", "/collections/modify", bytes.NewBufferString(body))
		r = authentication.SetUserInRequest(r, owner)
		w = httptest.NewRecorder()
		modifyCollection(w, r)
		test.IsEqualInt(t, w.Code, http.StatusOK)

		if newPassword != "" {
			// Still protected, and the old unlock no longer opens it
			test.IsEqualBool(t, boardUnlocked(), false)
			test.IsEqualBool(t, fileServed(), false)
			test.IsEqualBool(t, canView(), false)
		} else {
			// No password any more: the cookie is irrelevant for the board,
			// but the token itself is gone
			test.IsEqualBool(t, access.ValidCollectionPwCookie(withCookie("GET", "/"), collection.Id), false)
		}
	}

	// A rename that leaves the password alone keeps existing unlocks
	collection, err := storage.CreateCollection(storage.CollectionParameters{
		FileIds: []string{f.Id}, Password: "old"}, owner)
	test.IsNil(t, err)
	w := httptest.NewRecorder()
	writeCollectionPwCookie(w, collection)
	cookie := w.Result().Cookies()[0]
	r := httptest.NewRequest("POST", "/collections/modify", bytes.NewBufferString(`{"id":"`+collection.Id+`","name":"renamed"}`))
	w = httptest.NewRecorder()
	modifyCollection(w, authentication.SetUserInRequest(r, owner))
	test.IsEqualInt(t, w.Code, http.StatusOK)
	check := httptest.NewRequest("GET", "/", nil)
	check.AddCookie(cookie)
	test.IsEqualBool(t, access.ValidCollectionPwCookie(check, collection.Id), true)
}

// The Collections tab shows each collection's members in the shared file list
// (context collection-admin, List by default, one browser per collection) with
// Remove from collection, and nothing of the old member <ul>.
func TestCollectionsAdminPageUsesFileBrowser(t *testing.T) {
	one := accessFile("coladmfile000001", func(f *models.File) { f.Name = "first-member.pdf"; f.SHA1 = "coladmsha1" })
	two := accessFile("coladmfile000002", func(f *models.File) { f.Name = "second-member.pdf"; f.SHA1 = "coladmsha2" })
	owner := models.User{Id: 7}
	a, err := storage.CreateCollection(storage.CollectionParameters{Name: "adm-a", FileIds: []string{one.Id, two.Id}}, owner)
	test.IsNil(t, err)
	b, err := storage.CreateCollection(storage.CollectionParameters{Name: "adm-b", FileIds: []string{one.Id}}, owner)
	test.IsNil(t, err)
	// Someone else's collection is not listed to a user without PERM_LIST
	otherFile := accessFile("coladmfile000003", func(f *models.File) { f.Name = "foreign-member.pdf"; f.UserId = 5; f.SHA1 = "coladmsha3" })
	foreign, err := storage.CreateCollection(storage.CollectionParameters{Name: "adm-foreign", FileIds: []string{otherFile.Id}}, models.User{Id: 5})
	test.IsNil(t, err)

	test.HttpPageResult(t, test.HttpTestConfig{
		Url:     "http://localhost:53843/collections",
		IsHtml:  true,
		Cookies: []test.Cookie{{Name: "session_token", Value: "validsession"}},
		RequiredContent: []string{
			`id="col-` + a.Id + `"`, `id="col-` + b.Id + `"`,
			`data-context="collection-admin"`, `data-default-view="list"`,
			`data-collection-id="` + a.Id + `"`, `data-collection-id="` + b.Id + `"`,
			`data-menu="remove-from-collection"`, `data-act="remove-from-collection"`,
			"first-member.pdf", "second-member.pdf",
			`id="fgs-viewer"`, `id="fgs-collect-modal"`, `id="fgs-expiry-modal"`, "collection-addfiles",
		},
		ExcludedContent: []string{"member-remove", "collection-member-list", foreign.Id, "foreign-member.pdf"},
	})
}

// Remove from collection posts removeFileIds: the named members go, every
// other member stays (even one a stale page never knew about), and removing
// the last one is refused.
func TestCollectionModifyRemoveFileIds(t *testing.T) {
	owner := models.User{Id: 7}
	var ids []string
	for _, n := range []string{"1", "2", "3"} {
		ids = append(ids, accessFile("colrmfile00000"+n, func(f *models.File) { f.SHA1 = "colrmsha" + n; f.SizeBytes = 1024 }).Id)
	}
	collection, err := storage.CreateCollection(storage.CollectionParameters{Name: "rm", FileIds: ids}, owner)
	test.IsNil(t, err)
	modify := func(body map[string]any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		r := authentication.SetUserInRequest(httptest.NewRequest("POST", "/collections/modify", bytes.NewReader(raw)), owner)
		w := httptest.NewRecorder()
		modifyCollection(w, r)
		result := map[string]any{}
		test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
		return w.Code, result
	}

	code, result := modify(map[string]any{"id": collection.Id, "removeFileIds": []string{ids[0], "notAMember"}})
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualInt(t, int(result["Files"].(float64)), 2)
	test.IsEqualInt(t, int(result["FileCount"].(float64)), 2)
	test.IsEqualString(t, result["TotalSize"].(string), "2.0 kB")
	saved, _ := database.GetCollection(collection.Id)
	test.IsEqualString(t, strings.Join(saved.FileIds, ","), ids[1]+","+ids[2])

	code, _ = modify(map[string]any{"id": collection.Id, "removeFileIds": []string{ids[1], ids[2]}})
	test.IsEqualInt(t, code, http.StatusBadRequest)
	saved, _ = database.GetCollection(collection.Id)
	test.IsEqualInt(t, len(saved.FileIds), 2)

	// The form the page used before still replaces the member list
	code, result = modify(map[string]any{"id": collection.Id, "fileIds": []string{ids[2]}})
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualInt(t, int(result["Files"].(float64)), 1)
}
