//go:build !integration && test

package webserver

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// groupPageFixture is one group with three files in it and one outside:
//
//	A  anna's, uploaded 100, taken 3000
//	B  carla's, uploaded 200, taken 1000, a dHash two bits from A's: B is
//	   the newer of the pair, so carla is the one asked about it
//	C  carla's, uploaded 5000, no preview
//	D  anna's, not in the group
//
// Shooting order is B, A, C: neither upload order nor its reverse.
type groupPageFixture struct {
	group                    models.Group
	creator, member, outside models.User
	admin                    models.User
	a, b, c, d               models.File
}

func newGroupPageFixture(t *testing.T) groupPageFixture {
	t.Helper()
	fx := groupPageFixture{
		creator: models.User{Id: 71, Name: "carla@example.com", UserLevel: models.UserLevelUser},
		member:  models.User{Id: 72, Name: "anna@example.com", UserLevel: models.UserLevelUser},
		outside: models.User{Id: 73, Name: "zed@example.com", UserLevel: models.UserLevelUser},
		admin:   models.User{Id: 5, Name: "Test", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll},
	}
	for _, u := range []models.User{fx.creator, fx.member, fx.outside} {
		database.SaveUser(u, false)
	}
	g, err := storage.CreateGroup("Iceland trip", 30, fx.creator)
	test.IsNil(t, err)
	_, err = storage.JoinGroupByToken(g.InviteToken, fx.member)
	test.IsNil(t, err)
	fx.group = g

	file := func(id string, owner int, uploaded int64) models.File {
		return accessFile(id, func(f *models.File) {
			f.SHA1 = "sha-" + id
			f.UserId = owner
			f.UploadDate = uploaded
			f.Name = id + ".jpg"
		})
	}
	fx.a = file("grouppagea000000001", fx.member.Id, 100)
	fx.b = file("grouppageb000000001", fx.creator.Id, 200)
	fx.c = file("grouppagec000000001", fx.creator.Id, 5000)
	fx.d = file("grouppaged000000001", fx.member.Id, 300)
	const hash = uint64(0xF0F0F0F0F0F0F0F0)
	database.SavePreview(models.Preview{Sha1: fx.a.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage,
		Width: 4, Height: 3, TakenAt: 3000, PHash: hash ^ 0b11, Updated: 1})
	database.SavePreview(models.Preview{Sha1: fx.b.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage,
		Width: 4, Height: 3, TakenAt: 1000, PHash: hash, Updated: 1})
	added, err := storage.AddFilesToGroup(g, []string{fx.a.Id, fx.b.Id, fx.c.Id}, fx.admin)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(added.Added), 3)

	t.Cleanup(func() {
		database.DeleteGroup(g.Id)
		for _, f := range []models.File{fx.a, fx.b, fx.c, fx.d} {
			database.DeletePreview(f.SHA1)
			database.DeleteDownloadsOfFile(f.Id)
			database.RemoveFileFromGroups(f.Id)
			database.DeleteMetaData(f.Id)
		}
		for _, u := range []models.User{fx.creator, fx.member, fx.outside} {
			database.DeleteUser(u.Id)
		}
	})
	return fx
}

// groupPageCall runs a handler as this user, as requireLogin would
func groupPageCall(handler http.HandlerFunc, method, target string, user models.User, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, target, reader)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler(w, authentication.SetUserInRequest(r, user))
	return w
}

// tileOf returns the opening tag of a file's tile on the page
func tileOf(t *testing.T, page, fileId string) string {
	t.Helper()
	start := strings.Index(page, `<figure class="fgs-tile"`+` data-mine=`)
	for start >= 0 {
		end := strings.Index(page[start:], ">")
		tag := page[start : start+end]
		if strings.Contains(tag, `data-id="`+fileId+`"`) {
			return tag
		}
		next := strings.Index(page[start+1:], `<figure class="fgs-tile"`)
		if next < 0 {
			break
		}
		start += 1 + next
	}
	t.Fatalf("no tile for %s", fileId)
	return ""
}

// menuOf returns the opening tag of a file's first "⋯" menu on the page
func menuOf(t *testing.T, page, fileId string) string {
	t.Helper()
	start := strings.Index(page, `data-fgs-file-menu data-id="`+fileId+`"`)
	if start < 0 {
		t.Fatalf("no menu for %s", fileId)
	}
	end := strings.Index(page[start:], ">")
	return page[start : start+end]
}

// downloadRows counts the Downloads rows of these files for the user and
// route. No database function returns Via, so the test reads the table.
func downloadRows(t *testing.T, userId int, via string, fileIds ...string) int {
	t.Helper()
	db, err := sql.Open("sqlite", strings.TrimPrefix(testconfiguration.SqliteUrl, "sqlite://")+"?_pragma=busy_timeout=30000")
	test.IsNil(t, err)
	defer db.Close()
	count := 0
	for _, id := range fileIds {
		var n int
		err = db.QueryRow(`SELECT COUNT(*) FROM "Downloads" WHERE UserId = ? AND Via = ? AND FileId = ?`, userId, via, id).Scan(&n)
		test.IsNil(t, err)
		count += n
	}
	return count
}

func TestGroupPage(t *testing.T) {
	fx := newGroupPageFixture(t)
	page := "/g?id=" + fx.group.Id

	t.Run("a member sees the header and the board", func(t *testing.T) {
		w := groupPageCall(showGroup, "GET", page, fx.member, "")
		test.IsEqualInt(t, w.Code, http.StatusOK)
		body := w.Body.String()
		for _, want := range []string{"Iceland trip", "closes ", storage.GroupCode(fx.group),
			storage.GroupInviteUrl(fx.group), `data-context="group"`, `data-default-view="board"`,
			"Select everything new to me", "Hide what I&#39;ve downloaded", "Leave group",
			`data-fgs-upload-target="` + fx.group.Id + `"`, "FGS.initGroupUploadArea"} {
			if !strings.Contains(body, want) {
				t.Errorf("the member's page lacks %q", want)
			}
		}
		// Names are display names (D5), never the email
		test.IsEqualBool(t, strings.Contains(body, ">carla<"), true)
		test.IsEqualBool(t, strings.Contains(body, "carla@example.com"), false)
		// The creator's controls are not offered to a member
		test.IsEqualBool(t, strings.Contains(body, "Reset code"), false)
		test.IsEqualBool(t, strings.Contains(body, "Delete group"), false)
		test.IsEqualBool(t, strings.Contains(body, "You are not a member of this group"), false)
		// The page singletons are there once
		test.IsEqualInt(t, strings.Count(body, `id="modaledit"`), 1)
	})

	t.Run("items are in shooting order", func(t *testing.T) {
		body := groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		ia := strings.Index(body, `data-id="`+fx.a.Id+`"`)
		ib := strings.Index(body, `data-id="`+fx.b.Id+`"`)
		ic := strings.Index(body, `data-id="`+fx.c.Id+`"`)
		test.IsEqualBool(t, ia > 0 && ib > 0 && ic > 0, true)
		test.IsEqualBool(t, ib < ia, true) // taken 1000 before taken 3000
		test.IsEqualBool(t, ia < ic, true) // no date taken: uploaded 5000
		test.IsEqualBool(t, strings.Contains(body, `data-id="`+fx.d.Id+`"`), false)
	})

	t.Run("remove is offered on own items, and on all to the creator", func(t *testing.T) {
		body := groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.a.Id), `data-can-remove="true"`), true)
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.a.Id), `data-can-delete="true"`), true)
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.b.Id), `data-can-remove="false"`), true)
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.b.Id), `data-can-delete="false"`), true)
		// Edit stays (D4 revision); the group's terms are locked
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.b.Id), `data-group-lock="Iceland trip"`), true)
		body = groupPageCall(showGroup, "GET", page, fx.creator, "").Body.String()
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.a.Id), `data-can-remove="true"`), true)
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.a.Id), `data-can-delete="false"`), true)
		for _, want := range []string{"Rename", "Extend", "Reset code", "Delete group", `data-group-remove-member="72"`} {
			if !strings.Contains(body, want) {
				t.Errorf("the creator's page lacks %q", want)
			}
		}
		// The creator cannot leave, nor be removed
		test.IsEqualBool(t, strings.Contains(body, "Leave group"), false)
		test.IsEqualBool(t, strings.Contains(body, `data-group-remove-member="71"`), false)
	})

	t.Run("a stranger gets 404, the admin a banner", func(t *testing.T) {
		w := groupPageCall(showGroup, "GET", page, fx.outside, "")
		test.IsEqualInt(t, w.Code, http.StatusNotFound)
		test.IsEqualBool(t, strings.Contains(w.Body.String(), "Iceland trip"), false)
		w = groupPageCall(showGroup, "GET", "/g?id=nosuchgroup", fx.member, "")
		test.IsEqualInt(t, w.Code, http.StatusNotFound)

		w = groupPageCall(showGroup, "GET", page, fx.admin, "")
		test.IsEqualInt(t, w.Code, http.StatusOK)
		body := w.Body.String()
		test.IsEqualBool(t, strings.Contains(body, "You are not a member of this group"), true)
		test.IsEqualBool(t, strings.Contains(body, "Reset code"), true)
		test.IsEqualBool(t, strings.Contains(body, "Leave group"), false)
	})

	t.Run("a near-copy is flagged to the newer item's uploader only", func(t *testing.T) {
		body := groupPageCall(showGroup, "GET", page, fx.creator, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, `data-near-file="`+fx.b.Id+`"`), true)
		test.IsEqualBool(t, strings.Contains(body, "Looks like anna's photo from"), true)
		test.IsEqualBool(t, strings.Contains(body, "Keep both"), true)
		test.IsEqualBool(t, strings.Contains(body, "Remove mine"), true)
		// anna's A was there first: she is never asked about the pair
		body = groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, "data-near-file="), false)
		test.IsEqualBool(t, strings.Contains(body, "Keep both"), false)
	})

	t.Run("the person filter lists everyone with items, former members last", func(t *testing.T) {
		former := models.User{Id: 74, Name: "olaf@example.com", UserLevel: models.UserLevelUser}
		database.SaveUser(former, false)
		defer database.DeleteUser(former.Id)
		_, err := storage.JoinGroupByToken(fx.group.InviteToken, former)
		test.IsNil(t, err)
		e := accessFile("grouppagee000000001", func(f *models.File) {
			f.SHA1 = "sha-grouppagee"
			f.UserId = former.Id
			f.UploadDate = 50
		})
		defer func() {
			database.RemoveFileFromGroups(e.Id)
			database.DeleteMetaData(e.Id)
		}()
		added, err := storage.AddFilesToGroup(fx.group, []string{e.Id}, former)
		test.IsNil(t, err)
		test.IsEqualInt(t, len(added.Added), 1)
		test.IsNil(t, storage.LeaveGroup(fx.group, former))

		body := groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		carla := strings.Index(body, `<option value="71">carla</option>`)
		anna := strings.Index(body, `<option value="72">anna</option>`)
		olaf := strings.Index(body, `<option value="74">olaf</option>`)
		test.IsEqualBool(t, carla > 0 && anna > 0 && olaf > 0, true)
		test.IsEqualBool(t, carla < anna && anna < olaf, true) // members in member order, then former members
		test.IsEqualInt(t, strings.Count(body, `<option value="74">`), 1)
		// Olaf is no longer in the header
		test.IsEqualBool(t, strings.Contains(body, `data-group-person="74"`), false)
		test.IsEqualBool(t, strings.Contains(body, `data-group-person="72"`), true)
	})
}

func TestGroupDownloads(t *testing.T) {
	fx := newGroupPageFixture(t)
	g := fx.group

	t.Run("a member's /gd is recorded via group and ticks the tile", func(t *testing.T) {
		w := groupPageCall(downloadGroupFile, "GET", "/gd?id="+g.Id+"&file="+fx.a.Id, fx.member, "")
		test.IsEqualInt(t, w.Code, http.StatusOK)
		test.IsEqualInt(t, w.Body.Len(), 4096)
		test.IsEqualBool(t, database.GetDownloadedFileIds(fx.member.Id)[fx.a.Id], true)
		test.IsEqualInt(t, downloadRows(t, fx.member.Id, models.DownloadViaGroup, fx.a.Id), 1)

		body := groupPageCall(showGroup, "GET", "/g?id="+g.Id, fx.member, "").Body.String()
		test.IsEqualBool(t, strings.Contains(tileOf(t, body, fx.a.Id), `data-downloaded="true"`), true)
		test.IsEqualBool(t, strings.Contains(tileOf(t, body, fx.b.Id), `data-downloaded="false"`), true)
		// Someone else's download is not mine
		body = groupPageCall(showGroup, "GET", "/g?id="+g.Id, fx.creator, "").Body.String()
		test.IsEqualBool(t, strings.Contains(tileOf(t, body, fx.a.Id), `data-downloaded="false"`), true)
	})

	t.Run("/gd refuses strangers and files outside the group", func(t *testing.T) {
		test.IsEqualInt(t, groupPageCall(downloadGroupFile, "GET", "/gd?id="+g.Id+"&file="+fx.a.Id, fx.outside, "").Code, http.StatusNotFound)
		test.IsEqualInt(t, groupPageCall(downloadGroupFile, "GET", "/gd?id="+g.Id+"&file="+fx.d.Id, fx.member, "").Code, http.StatusNotFound)
		test.IsEqualInt(t, groupPageCall(downloadGroupFile, "GET", "/gd?id=nosuchgroup&file="+fx.a.Id, fx.member, "").Code, http.StatusNotFound)
		test.IsEqualInt(t, downloadRows(t, fx.outside.Id, models.DownloadViaGroup, fx.a.Id), 0)
		test.IsEqualInt(t, downloadRows(t, fx.member.Id, models.DownloadViaGroup, fx.d.Id), 0)
	})

	t.Run("/gz of two ids writes two zip rows", func(t *testing.T) {
		w := groupPageCall(downloadGroupZip, "GET", "/gz?id="+g.Id+"&files="+fx.b.Id+","+fx.c.Id+","+fx.d.Id, fx.member, "")
		test.IsEqualInt(t, w.Code, http.StatusOK)
		test.IsEqualString(t, w.Header().Get("Content-Type"), "application/zip")
		test.IsEqualInt(t, downloadRows(t, fx.member.Id, models.DownloadViaZip, fx.b.Id, fx.c.Id), 2)
		// D is not in the group: never resolved, never recorded
		test.IsEqualInt(t, downloadRows(t, fx.member.Id, models.DownloadViaZip, fx.d.Id), 0)
		test.IsEqualInt(t, downloadRows(t, fx.member.Id, models.DownloadViaZip, fx.a.Id), 0)
	})

	t.Run("/gz refuses strangers", func(t *testing.T) {
		w := groupPageCall(downloadGroupZip, "GET", "/gz?id="+g.Id+"&files="+fx.b.Id, fx.outside, "")
		test.IsEqualInt(t, w.Code, http.StatusNotFound)
		test.IsEqualInt(t, downloadRows(t, fx.outside.Id, models.DownloadViaZip, fx.b.Id), 0)
	})
}

func TestGroupItem(t *testing.T) {
	fx := newGroupPageFixture(t)
	g := fx.group
	item := func(user models.User, fileId string) (int, map[string]any) {
		w := groupPageCall(groupItem, "GET", "/groups/item?id="+g.Id+"&file="+fileId, user, "")
		if w.Code != http.StatusOK {
			return w.Code, nil
		}
		var result map[string]any
		test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
		return w.Code, result
	}

	status, got := item(fx.member, fx.b.Id)
	test.IsEqualInt(t, status, http.StatusOK)
	test.IsEqualString(t, got["Id"].(string), fx.b.Id)
	test.IsEqualString(t, got["Name"].(string), fx.b.Name)
	test.IsEqualString(t, got["UploaderName"].(string), "carla")
	test.IsEqualInt(t, int(got["UploaderId"].(float64)), fx.creator.Id)
	test.IsEqualString(t, got["GroupLock"].(string), "Iceland trip")
	test.IsEqualBool(t, got["Downloaded"].(bool), false)
	test.IsEqualBool(t, got["CanRemove"].(bool), false)
	test.IsEqualBool(t, got["UnlimitedDownloads"].(bool), true)
	preview := got["Preview"].(map[string]any)
	test.IsEqualString(t, preview["Status"].(string), models.PreviewReady)
	test.IsEqualInt(t, int(preview["TakenAt"].(float64)), 1000)
	test.IsEqualString(t, preview["Download"].(string), "./gd?id="+g.Id+"&file="+fx.b.Id)

	// Own items may be removed; the creator may remove any
	_, got = item(fx.member, fx.a.Id)
	test.IsEqualBool(t, got["CanRemove"].(bool), true)
	_, got = item(fx.creator, fx.a.Id)
	test.IsEqualBool(t, got["CanRemove"].(bool), true)

	// Not in the group, or not your group: 404
	status, _ = item(fx.member, fx.d.Id)
	test.IsEqualInt(t, status, http.StatusNotFound)
	status, _ = item(fx.outside, fx.a.Id)
	test.IsEqualInt(t, status, http.StatusNotFound)

	// Looking is not downloading
	test.IsEqualInt(t, len(database.GetDownloadedFileIds(fx.member.Id)), 0)
	test.IsEqualInt(t, len(database.GetDownloadedFileIds(fx.creator.Id)), 0)
}

func TestGroupPageNearKeep(t *testing.T) {
	fx := newGroupPageFixture(t)
	g := fx.group
	keep := func(user models.User, fileId string) int {
		body, err := json.Marshal(map[string]string{"id": g.Id, "fileId": fileId})
		test.IsNil(t, err)
		return groupPageCall(keepNearCopy, "POST", "/groups/nearkeep", user, string(body)).Code
	}

	test.IsEqualInt(t, groupPageCall(keepNearCopy, "GET", "/groups/nearkeep", fx.member, "").Code, http.StatusMethodNotAllowed)
	// Only the file's owner keeps it: not another member, nor the creator,
	// nor the admin
	test.IsEqualInt(t, keep(fx.member, fx.b.Id), http.StatusForbidden)
	test.IsEqualInt(t, keep(fx.creator, fx.a.Id), http.StatusForbidden)
	test.IsEqualInt(t, keep(fx.admin, fx.b.Id), http.StatusForbidden)
	test.IsEqualInt(t, keep(fx.member, fx.d.Id), http.StatusNotFound)
	test.IsEqualInt(t, keep(fx.outside, fx.b.Id), http.StatusNotFound)
	test.IsEqualInt(t, len(database.GetNearCopiesKept(g.Id)), 0)

	test.IsEqualInt(t, keep(fx.creator, fx.b.Id), http.StatusOK)
	test.IsEqualBool(t, database.GetNearCopiesKept(g.Id)[fx.b.Id], true)
	// Settled for good: neither side is asked about the pair again
	for _, u := range []models.User{fx.creator, fx.member} {
		body := groupPageCall(showGroup, "GET", "/g?id="+g.Id, u, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, "data-near-file="), false)
	}
}
