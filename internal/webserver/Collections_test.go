//go:build !integration && test

package webserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
