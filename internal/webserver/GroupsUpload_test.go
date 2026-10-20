//go:build !integration && test

package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/encryption"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// groupHasAs calls /groups/has as this user and returns the status and the body
func groupHasAs(t *testing.T, user models.User, query string) (int, groupHasResult) {
	t.Helper()
	w := httptest.NewRecorder()
	groupHas(w, authentication.SetUserInRequest(httptest.NewRequest("GET", "/groups/has"+query, nil), user))
	var result groupHasResult
	if w.Code == http.StatusOK {
		test.IsEqualString(t, w.Header().Get("cache-control"), "no-store, no-cache")
		test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
	}
	return w.Code, result
}

func TestGroupsHas(t *testing.T) {
	anna := models.User{Id: 31, Name: "anna@example.com", UserLevel: models.UserLevelUser}
	member := models.User{Id: 32, Name: "bert@example.com", UserLevel: models.UserLevelUser}
	stranger := models.User{Id: 33, Name: "carl@example.com", UserLevel: models.UserLevelUser}
	admin := models.User{Id: 5, Name: "Test", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	database.SaveUser(anna, false)
	database.SaveUser(member, false)
	database.SaveUser(stranger, false)

	g, err := storage.CreateGroup("Has", 30, anna)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	_, err = storage.JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)

	const sha = "0123456789abcdef0123456789abcdef01234567"
	f := accessFile("groupshas000000001", func(f *models.File) { f.SHA1 = sha; f.UserId = anna.Id })
	added, err := storage.AddFilesToGroup(g, []string{f.Id}, anna)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(added.Added), 1)

	// A member asking with the SHA1 of a group file: it is there, by anna
	status, result := groupHasAs(t, member, "?id="+g.Id+"&sha1="+sha)
	test.IsEqualInt(t, status, http.StatusOK)
	test.IsEqualBool(t, result.Checkable, true)
	test.IsEqualBool(t, result.Exists, true)
	test.IsEqualString(t, result.Uploader, "anna")
	test.IsEqualString(t, result.FileId, f.Id)

	// Upper case hex is the same fingerprint
	status, result = groupHasAs(t, member, "?id="+g.Id+"&sha1=0123456789ABCDEF0123456789ABCDEF01234567")
	test.IsEqualInt(t, status, http.StatusOK)
	test.IsEqualBool(t, result.Exists, true)

	// Another hash: not there
	status, result = groupHasAs(t, member, "?id="+g.Id+"&sha1=ffffffffffffffffffffffffffffffffffffffff")
	test.IsEqualInt(t, status, http.StatusOK)
	test.IsEqualBool(t, result.Checkable, true)
	test.IsEqualBool(t, result.Exists, false)
	test.IsEqualString(t, result.Uploader, "")
	test.IsEqualString(t, result.FileId, "")

	// The admin may ask too (D17)
	status, result = groupHasAs(t, admin, "?id="+g.Id+"&sha1="+sha)
	test.IsEqualInt(t, status, http.StatusOK)
	test.IsEqualBool(t, result.Exists, true)

	// A stranger, or an unknown group: 404, nothing revealed
	status, _ = groupHasAs(t, stranger, "?id="+g.Id+"&sha1="+sha)
	test.IsEqualInt(t, status, http.StatusNotFound)
	status, _ = groupHasAs(t, member, "?id=nosuchgroup&sha1="+sha)
	test.IsEqualInt(t, status, http.StatusNotFound)

	// Not a SHA1
	status, _ = groupHasAs(t, member, "?id="+g.Id+"&sha1=xyz")
	test.IsEqualInt(t, status, http.StatusBadRequest)

	// Stored hashes are salted when uploads are encrypted: nothing can be checked (D3)
	level := configuration.Get().Encryption.Level
	configuration.Get().Encryption.Level = encryption.FullEncryptionStored
	defer func() { configuration.Get().Encryption.Level = level }()
	status, result = groupHasAs(t, member, "?id="+g.Id+"&sha1="+sha)
	test.IsEqualInt(t, status, http.StatusOK)
	test.IsEqualBool(t, result.Checkable, false)
	test.IsEqualBool(t, result.Exists, false)
	test.IsEqualString(t, result.Uploader, "")
	configuration.Get().Encryption.Level = level

	// Unauthenticated, on the real server: 401
	resp, err := http.Get("http://localhost:53843/groups/has?id=" + g.Id + "&sha1=" + sha)
	test.IsNil(t, err)
	resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, http.StatusUnauthorized)
}

func TestGroupsHasUploadPage(t *testing.T) {
	// The upload page's list takes the finished uploads, and it has the
	// "Upload to" selector, which starts on My files
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/admin",
		RequiredContent: []string{
			`data-context="upload"`,
			`data-upload-sink="true"`,
			`<label for="fgs-upload-target" class="small text-muted text-nowrap">Upload to</label>`,
			`<option value="">My files</option>`,
			`autocomplete="off"`,
			"Files uploaded to a group stay until the group closes and can be downloaded by its members.",
			"initUploadTarget();",
		},
		IsHtml:  true,
		Cookies: []test.Cookie{{Name: "session_token", Value: "validsession"}},
	})

	// The group page's upload area: a drop zone fixed to its group
	var out strings.Builder
	test.IsNil(t, templateFolder.ExecuteTemplate(&out, "group_upload_area", `grp"1`))
	body := out.String()
	test.IsEqualBool(t, strings.Contains(body, `id="uploaddropzone"`), true)
	test.IsEqualBool(t, strings.Contains(body, `data-fgs-upload-target="grp&#34;1"`), true)
	test.IsEqualBool(t, strings.Contains(body, `id="uploadstatus"`), true)
}
