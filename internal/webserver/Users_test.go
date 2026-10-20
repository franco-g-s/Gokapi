//go:build !integration && test

package webserver

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// getPage is a logged-in GET of a page, returning its HTML
func getPage(t *testing.T, url string, cookie test.Cookie) string {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	test.IsNil(t, err)
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	resp, err := http.DefaultClient.Do(req)
	test.IsNil(t, err)
	defer resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
	body, err := io.ReadAll(resp.Body)
	test.IsNil(t, err)
	return string(body)
}

// userRow is the <tr> of one person on the Users page
func userRow(t *testing.T, page string, userId string) string {
	t.Helper()
	match := regexp.MustCompile(`(?s)<tr id="row-` + userId + `">.*?</tr>`).FindString(page)
	test.IsEqualBool(t, match != "", true)
	return match
}

func TestUsersPage(t *testing.T) {
	user := models.User{Id: 31, Name: "stats@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}
	database.SaveUser(user, false)
	defer database.DeleteUser(31)
	lonely := models.User{Id: 32, Name: "lonely@example.com", Permissions: models.UserPermissionNone, UserLevel: models.UserLevelUser}
	database.SaveUser(lonely, false)
	defer database.DeleteUser(32)
	defer func() {
		for _, id := range []string{"usersfile1", "usersfile2", "usersexpired", "userspending", "usersrequest", "usersother"} {
			database.DeleteMetaData(id)
		}
	}()
	mine := func(size int64) func(*models.File) {
		return func(f *models.File) { f.UserId = 31; f.SizeBytes = size }
	}
	accessFile("usersfile1", mine(1500))
	accessFile("usersfile2", mine(2500))
	// None of these is listed on the Files tab, so none is counted
	accessFile("usersexpired", func(f *models.File) {
		mine(100000)(f)
		f.ExpireAt = time.Now().Add(-time.Hour).Unix()
	})
	accessFile("userspending", func(f *models.File) {
		mine(100000)(f)
		f.PendingDeletion = time.Now().Add(time.Hour).Unix()
	})
	accessFile("usersrequest", func(f *models.File) {
		mine(100000)(f)
		f.UploadRequestId = "somerequest"
	})
	// Another person's file is not counted for this one
	accessFile("usersother", func(f *models.File) { f.UserId = 32; f.SizeBytes = 7 })
	trip, err := storage.CreateGroup("Iceland trip", 30, user)
	test.IsNil(t, err)
	defer database.DeleteGroup(trip.Id)
	hike, err := storage.CreateGroup("Hike", 0, user)
	test.IsNil(t, err)
	defer database.DeleteGroup(hike.Id)

	page := getPage(t, "http://localhost:53843/users", filesAdminSession())
	row := userRow(t, page, "31")
	test.IsEqualBool(t, strings.Contains(row, `<span class="user-uploadcount">2</span>`), true)
	test.IsEqualBool(t, strings.Contains(row, `href="./files?user=31"`), true)
	test.IsEqualBool(t, strings.Contains(row, "View files"), true)
	test.IsEqualBool(t, strings.Contains(row, "3.9 kB"), true)
	test.IsEqualBool(t, strings.Contains(row, "Iceland trip"), true)
	test.IsEqualBool(t, strings.Contains(row, "Hike"), true)

	// A person with one file in no group
	row = userRow(t, page, "32")
	test.IsEqualBool(t, strings.Contains(row, `<span class="user-uploadcount">1</span>`), true)
	test.IsEqualBool(t, strings.Contains(row, "7 B"), true)
	test.IsEqualBool(t, strings.Contains(row, "—"), true)
	test.IsEqualBool(t, strings.Contains(row, "Iceland trip"), false)

	// One count column: no separate Files column
	test.IsEqualBool(t, strings.Contains(page, `<th scope="col">Files</th>`), false)
	// The columns are in the header, in the order addRowUser builds them
	for _, heading := range []string{`<th scope="col">Uploads</th>`, `<th scope="col">Size</th>`, `<th scope="col">Groups</th>`} {
		test.IsEqualBool(t, strings.Contains(page, heading), true)
	}
}

func TestFilesForUser(t *testing.T) {
	accessFile("forusermine", func(f *models.File) { f.UserId = 7 })
	accessFile("foruseradmin", func(f *models.File) { f.UserId = 5 })

	// The admin sees only user 7's files, under "<name>'s files" with a way
	// back, and without the My files / Everything switch
	// SaveUser lowercases names, and other tests rename user 7: ask the database
	user7, ok := database.GetUser(7)
	test.IsEqualBool(t, ok, true)
	name := displayName(user7)
	page := getPage(t, "http://localhost:53843/files?user=7", filesAdminSession())
	test.IsEqualBool(t, strings.Contains(page, name+"'s files"), true)
	test.IsEqualBool(t, strings.Contains(page, `href="./users"`), true)
	test.IsEqualBool(t, strings.Contains(page, `data-id="forusermine"`), true)
	test.IsEqualBool(t, strings.Contains(page, `data-id="foruseradmin"`), false)
	test.IsEqualBool(t, strings.Contains(page, `data-scope="mine"`), false)
	test.IsEqualBool(t, strings.Contains(page, `data-scope="all"`), false)

	// Without the parameter nothing changes
	page = getPage(t, "http://localhost:53843/files", filesAdminSession())
	test.IsEqualBool(t, strings.Contains(page, `data-id="foruseradmin"`), true)
	test.IsEqualBool(t, strings.Contains(page, `data-scope="mine"`), true)
	test.IsEqualBool(t, strings.Contains(page, "'s files"), false)

	// A person who is not on the system is ignored
	page = getPage(t, "http://localhost:53843/files?user=9999", filesAdminSession())
	test.IsEqualBool(t, strings.Contains(page, `data-id="foruseradmin"`), true)
	page = getPage(t, "http://localhost:53843/files?user=abc", filesAdminSession())
	test.IsEqualBool(t, strings.Contains(page, `data-id="foruseradmin"`), true)

	// User 7 has no PERM_LIST: the parameter is ignored, they see their own files
	page = getPage(t, "http://localhost:53843/files?user=5", test.Cookie{Name: "session_token", Value: "validsession"})
	test.IsEqualBool(t, strings.Contains(page, `data-id="forusermine"`), true)
	test.IsEqualBool(t, strings.Contains(page, `data-id="foruseradmin"`), false)
	test.IsEqualBool(t, strings.Contains(page, "'s files"), false)
}
