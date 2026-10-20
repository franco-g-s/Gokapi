//go:build !integration && test

package webserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

func sharedSeen(userId int, collectionId string) bool {
	for _, s := range database.GetCollectionsSeen(userId) {
		if s.CollectionId == collectionId {
			return true
		}
	}
	return false
}

// openCollection requests /c as the holder of the session cookie (none when
// token is empty) and returns the response body.
func openCollection(collectionId, token string, extra ...*http.Cookie) string {
	r := httptest.NewRequest("GET", "/c?id="+collectionId, nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	}
	for _, c := range extra {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	showCollection(w, r)
	return w.Body.String()
}

func collectionsPageAs(t *testing.T, token string) string {
	t.Helper()
	req, err := http.NewRequest("GET", "http://localhost:53843/collections", nil)
	test.IsNil(t, err)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	resp, err := http.DefaultClient.Do(req)
	test.IsNil(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	test.IsNil(t, err)
	return string(body)
}

// A signed-in visitor who opens someone else's collection finds it under
// "Shared with me". Anonymous visitors, the owner and the password form
// record nothing.
func TestSharedWithMe(t *testing.T) {
	database.SaveUser(models.User{Id: 8, Name: "anna@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveSession("sharedanna", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 8})
	database.SaveUser(models.User{Id: 7, Name: "ben@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)

	f := accessFile("sharedwithme00001", func(f *models.File) { f.Name = "trip.jpg"; f.SHA1 = "sharedwithmesha1" })
	owner := models.User{Id: 7}
	collection, err := storage.CreateCollection(storage.CollectionParameters{Name: "Iceland trip", FileIds: []string{f.Id}}, owner)
	test.IsNil(t, err)

	// Anonymous: nothing recorded, and the page still opens
	body := openCollection(collection.Id, "")
	test.IsEqualBool(t, strings.Contains(body, "./cz?id="+collection.Id), true)
	test.IsEqualInt(t, len(database.GetCollectionsSeen(8)), 0)

	// The owner opening their own collection records nothing
	openCollection(collection.Id, "validsession")
	test.IsEqualBool(t, sharedSeen(7, collection.Id), false)

	// A page without it for user 8
	page := collectionsPageAs(t, "sharedanna")
	test.IsEqualBool(t, strings.Contains(page, "Shared with me"), false)

	// User 8 opens it: remembered, and listed
	body = openCollection(collection.Id, "sharedanna")
	test.IsEqualBool(t, strings.Contains(body, "./cz?id="+collection.Id), true)
	test.IsEqualBool(t, sharedSeen(8, collection.Id), true)
	page = collectionsPageAs(t, "sharedanna")
	test.IsEqualBool(t, strings.Contains(page, "Shared with me"), true)
	test.IsEqualBool(t, strings.Contains(page, "Iceland trip"), true)
	test.IsEqualBool(t, strings.Contains(page, `href="./c?id=`+collection.Id+`"`), true)
	// The owner is shown by display name, never the full address
	test.IsEqualBool(t, strings.Contains(page, "ben"), true)
	test.IsEqualBool(t, strings.Contains(page, "ben@example.com"), false)

	// The owner's page does not list their own collection as shared
	test.IsEqualBool(t, strings.Contains(collectionsPageAs(t, "validsession"), "Shared with me"), false)

	// Opening it again keeps the first time
	first := database.GetCollectionsSeen(8)[0].FirstOpened
	openCollection(collection.Id, "sharedanna")
	test.IsEqualInt64(t, database.GetCollectionsSeen(8)[0].FirstOpened, first)

	// Deleting the collection removes it from the list
	r := httptest.NewRequest("POST", "/collections/delete?id="+collection.Id, nil)
	w := httptest.NewRecorder()
	deleteCollection(w, authentication.SetUserInRequest(r, owner))
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualBool(t, sharedSeen(8, collection.Id), false)
	test.IsEqualBool(t, strings.Contains(collectionsPageAs(t, "sharedanna"), "Iceland trip"), false)
}

// A password-protected collection opened without the unlock cookie shows the
// form and records nothing; once unlocked, the visit counts.
func TestSharedWithMePasswordGate(t *testing.T) {
	database.SaveUser(models.User{Id: 8, Name: "anna@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}, false)
	database.SaveSession("sharedanna", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: 8})
	f := accessFile("sharedwithme00002", func(f *models.File) { f.Name = "secret.jpg"; f.SHA1 = "sharedwithmesha2" })
	collection, err := storage.CreateCollection(storage.CollectionParameters{
		Name: "Locked", FileIds: []string{f.Id}, Password: "pw"}, models.User{Id: 7})
	test.IsNil(t, err)

	body := openCollection(collection.Id, "sharedanna")
	test.IsEqualBool(t, strings.Contains(body, "./cz?id="+collection.Id), false)
	test.IsEqualBool(t, sharedSeen(8, collection.Id), false)

	w := httptest.NewRecorder()
	writeCollectionPwCookie(w, collection)
	body = openCollection(collection.Id, "sharedanna", w.Result().Cookies()[0])
	test.IsEqualBool(t, strings.Contains(body, "./cz?id="+collection.Id), true)
	test.IsEqualBool(t, sharedSeen(8, collection.Id), true)
}
