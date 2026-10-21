//go:build !integration && test

package webserver

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
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
		Width: 4, Height: 3, Color: "#3060d0", TakenAt: 3000, PHash: hash ^ 0b11, Updated: 1})
	database.SavePreview(models.Preview{Sha1: fx.b.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage,
		Width: 4, Height: 3, Color: "#3060d0", TakenAt: 1000, PHash: hash, Updated: 1})
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
		test.IsEqualBool(t, strings.Contains(body, "groupRemoveMemberModal"), false)
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
		for _, want := range []string{"Rename", "Extend", "Reset code", "Delete group", `data-group-remove-member="72"`,
			// I4: removing someone offers to reset the code, ticked
			`id="groupRemoveMemberModal"`, `id="groupRemoveMemberReset" checked`, "Also reset the invite code and link"} {
			if !strings.Contains(body, want) {
				t.Errorf("the creator's page lacks %q", want)
			}
		}
		// The creator cannot leave, nor be removed
		test.IsEqualBool(t, strings.Contains(body, "Leave group"), false)
		test.IsEqualBool(t, strings.Contains(body, `data-group-remove-member="71"`), false)
	})

	t.Run("management data only for the files the viewer may edit (I2)", func(t *testing.T) {
		// anna (member) views carla's B: no /d link, no data-url, no counts
		body := groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		menuB := menuOf(t, body, fx.b.Id)
		for _, leak := range []string{"data-url", "data-downloads", "data-expire", "data-password", "data-can-edit"} {
			test.IsEqualBool(t, strings.Contains(menuB, leak), false)
		}
		test.IsEqualBool(t, strings.Contains(body, "/d?id="+fx.b.Id), false)
		test.IsEqualBool(t, strings.Contains(body, "/d?id="+fx.c.Id), false)
		// Her own A keeps them, so Edit and Replace still work
		menuA := menuOf(t, body, fx.a.Id)
		test.IsEqualBool(t, strings.Contains(menuA, `data-url="`), true)
		test.IsEqualBool(t, strings.Contains(menuA, `data-can-edit="true"`), true)
		test.IsEqualBool(t, strings.Contains(body, "/d?id="+fx.a.Id), true)
		// No Expires or Downloads columns on a group page
		test.IsEqualBool(t, strings.Contains(body, "<th>Expires</th>"), false)
		test.IsEqualBool(t, strings.Contains(body, "<th>Downloads</th>"), false)
		test.IsEqualBool(t, strings.Contains(body, `data-col="downloads"`), false)

		// carla's own view of the same page: B and C carry theirs, A does not
		body = groupPageCall(showGroup, "GET", page, fx.creator, "").Body.String()
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.b.Id), `data-url="`), true)
		test.IsEqualBool(t, strings.Contains(body, "/d?id="+fx.b.Id), true)
		test.IsEqualBool(t, strings.Contains(menuOf(t, body, fx.a.Id), "data-url"), false)
		test.IsEqualBool(t, strings.Contains(body, "/d?id="+fx.a.Id), false)
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

	t.Run("a near-copy is flagged to the newer item's uploader, creator and admin", func(t *testing.T) {
		// carla uploaded B, the newer: hers to keep or remove
		body := groupPageCall(showGroup, "GET", page, fx.creator, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, `data-near-file="`+fx.b.Id+`"`), true)
		test.IsEqualBool(t, strings.Contains(body, "Looks like anna&#39;s photo from") || strings.Contains(body, "Looks like anna's photo from"), true)
		test.IsEqualBool(t, strings.Contains(body, "Keep both"), true)
		test.IsEqualBool(t, strings.Contains(body, "Remove mine"), true)
		test.IsEqualBool(t, strings.Contains(body, "Remove newer"), false)
		test.IsEqualBool(t, strings.Contains(body, "(uploaded by"), false)
		// Both thumbnails and a Compare button open the full-screen compare
		test.IsEqualInt(t, strings.Count(body, `data-near-open=`), 2)
		test.IsEqualBool(t, strings.Contains(body, `data-near-compare="`+fx.b.Id+`"`), true)
		// anna's A was there first: she is never asked about the pair
		body = groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, "data-near-file="), false)
		test.IsEqualBool(t, strings.Contains(body, "Keep both"), false)
		// The admin uploaded neither and sees it, labelled with who uploaded the newer
		body = groupPageCall(showGroup, "GET", page, fx.admin, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, `data-near-file="`+fx.b.Id+`"`), true)
		test.IsEqualBool(t, strings.Contains(body, "(uploaded by carla)"), true)
		test.IsEqualBool(t, strings.Contains(body, "Remove newer"), true)
		test.IsEqualBool(t, strings.Contains(body, "Remove mine"), false)
	})

	t.Run("the creator sees a pair of two other members, a plain member does not", func(t *testing.T) {
		olaf := models.User{Id: 75, Name: "olaf@example.com", UserLevel: models.UserLevelUser}
		database.SaveUser(olaf, false)
		_, err := storage.JoinGroupByToken(fx.group.InviteToken, olaf)
		test.IsNil(t, err)
		defer func() {
			test.IsNil(t, storage.LeaveGroup(fx.group, olaf))
			database.DeleteUser(olaf.Id)
		}()
		// olaf's F is newer than anna's A and one dHash bit from it; carla's B is
		// three bits away, so A is the closest match
		f := accessFile("grouppagef000000001", func(f *models.File) {
			f.SHA1 = "sha-grouppagef"
			f.UserId = olaf.Id
			f.UploadDate = 400
			f.Name = "f.jpg"
		})
		database.SavePreview(models.Preview{Sha1: f.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage,
			Width: 4, Height: 3, Color: "#3060d0", TakenAt: 3500, PHash: uint64(0xF0F0F0F0F0F0F0F0) ^ 0b111, Updated: 1})
		defer func() {
			database.DeletePreview(f.SHA1)
			database.RemoveFileFromGroups(f.Id)
			database.DeleteMetaData(f.Id)
		}()
		added, err := storage.AddFilesToGroup(fx.group, []string{f.Id}, olaf)
		test.IsNil(t, err)
		test.IsEqualInt(t, len(added.Added), 1)

		body := groupPageCall(showGroup, "GET", page, fx.creator, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, `data-near-file="`+f.Id+`"`), true)
		test.IsEqualBool(t, strings.Contains(body, "(uploaded by olaf)"), true)
		body = groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, `data-near-file="`+f.Id+`"`), false)
		// olaf is asked about his own
		body = groupPageCall(showGroup, "GET", page, olaf, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, `data-near-file="`+f.Id+`"`), true)
		test.IsEqualBool(t, strings.Contains(body, "Remove mine"), true)
	})

	t.Run("a pair in two colourings of one design is not flagged", func(t *testing.T) {
		database.SavePreview(models.Preview{Sha1: fx.a.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage,
			Width: 4, Height: 3, Color: "#d060a0", TakenAt: 3000, PHash: uint64(0xF0F0F0F0F0F0F0F0) ^ 0b11, Updated: 1})
		defer database.SavePreview(models.Preview{Sha1: fx.a.SHA1, Status: models.PreviewReady, Kind: models.PreviewKindImage,
			Width: 4, Height: 3, Color: "#3060d0", TakenAt: 3000, PHash: uint64(0xF0F0F0F0F0F0F0F0) ^ 0b11, Updated: 1})
		for _, u := range []models.User{fx.creator, fx.admin} {
			body := groupPageCall(showGroup, "GET", page, u, "").Body.String()
			test.IsEqualBool(t, strings.Contains(body, "data-near-file="), false)
		}
	})

	t.Run("the members panel lists everyone, behind a stack button", func(t *testing.T) {
		body := groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		for _, want := range []string{`id="groupMembersButton"`, `data-bs-toggle="dropdown"`, `aria-label="Members: 2"`,
			"2 members", `<h6 class="dropdown-header`, `<ul class="fgs-group-people`,
			`data-group-person="71"`, `data-group-person="72"`, ">Creator<"} {
			if !strings.Contains(body, want) {
				t.Errorf("the member's page lacks %q", want)
			}
		}
		// Two people: no overflow, and no remove buttons for a member
		test.IsEqualBool(t, strings.Contains(body, "fgs-avatar-more"), false)
		test.IsEqualBool(t, strings.Contains(body, "data-group-remove-member"), false)
		test.IsEqualInt(t, strings.Count(body, `class="fgs-avatar" style=`), 4) // the stack, then the panel
	})

	t.Run("more than five members: five avatars, +N and the count", func(t *testing.T) {
		var extra []models.User
		for i := 0; i < 5; i++ {
			u := models.User{Id: 80 + i, Name: "extra" + strconv.Itoa(i) + "@example.com", UserLevel: models.UserLevelUser}
			database.SaveUser(u, false)
			extra = append(extra, u)
			_, err := storage.JoinGroupByToken(fx.group.InviteToken, u)
			test.IsNil(t, err)
		}
		defer func() {
			for _, u := range extra {
				test.IsNil(t, storage.LeaveGroup(fx.group, u))
				database.DeleteUser(u.Id)
			}
		}()
		// 7 members: creator, anna and five more
		body := groupPageCall(showGroup, "GET", page, fx.creator, "").Body.String()
		for _, want := range []string{`aria-label="Members: 7"`, "7 members", ">+2<"} {
			if !strings.Contains(body, want) {
				t.Errorf("the creator's page lacks %q", want)
			}
		}
		test.IsEqualInt(t, strings.Count(body, `data-group-person=`), 7) // every member in the panel
		for _, u := range extra {
			test.IsEqualBool(t, strings.Contains(body, `data-group-remove-member="`+strconv.Itoa(u.Id)+`"`), true)
		}
		// Not shown to a member who cannot manage
		body = groupPageCall(showGroup, "GET", page, fx.member, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, "data-group-remove-member"), false)
		test.IsEqualBool(t, strings.Contains(body, ">+2<"), true)
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

	t.Run("/gz without files= is every live member (I3: select all)", func(t *testing.T) {
		w := groupPageCall(downloadGroupZip, "GET", "/gz?id="+g.Id, fx.creator, "")
		test.IsEqualInt(t, w.Code, http.StatusOK)
		archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		test.IsNil(t, err)
		names := make([]string, 0, len(archive.File))
		for _, entry := range archive.File {
			names = append(names, entry.Name)
		}
		sort.Strings(names)
		test.IsEqualString(t, strings.Join(names, ","), fx.a.Name+","+fx.b.Name+","+fx.c.Name)
		test.IsEqualInt(t, downloadRows(t, fx.creator.Id, models.DownloadViaZip, fx.a.Id, fx.b.Id, fx.c.Id), 3)
		test.IsEqualInt(t, downloadRows(t, fx.creator.Id, models.DownloadViaZip, fx.d.Id), 0)
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
	// Someone else's file carries none of its management data (I2)
	test.IsEqualString(t, got["UrlDownload"].(string), "")
	test.IsEqualString(t, got["UrlHotlink"].(string), "")
	test.IsEqualInt(t, int(got["DownloadCount"].(float64)), 0)
	test.IsEqualInt(t, int(got["ExpireAt"].(float64)), 0)
	preview := got["Preview"].(map[string]any)
	test.IsEqualString(t, preview["Status"].(string), models.PreviewReady)
	test.IsEqualInt(t, int(preview["TakenAt"].(float64)), 1000)
	test.IsEqualString(t, preview["Download"].(string), "./gd?id="+g.Id+"&file="+fx.b.Id)

	// Own items may be removed; the creator may remove any
	_, got = item(fx.member, fx.a.Id)
	test.IsEqualBool(t, got["CanRemove"].(bool), true)
	// and keep their management data
	test.IsEqualBool(t, strings.Contains(got["UrlDownload"].(string), "/d?id="+fx.a.Id), true)
	test.IsEqualBool(t, got["UnlimitedDownloads"].(bool), true)
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
	// The file's uploader keeps it, and so do the group's creator and the
	// admin, who see every pair: not another member
	test.IsEqualInt(t, keep(fx.member, fx.b.Id), http.StatusForbidden)
	test.IsEqualInt(t, keep(fx.outside, fx.a.Id), http.StatusNotFound)
	test.IsEqualInt(t, keep(fx.member, fx.d.Id), http.StatusNotFound)
	test.IsEqualInt(t, keep(fx.outside, fx.b.Id), http.StatusNotFound)
	test.IsEqualInt(t, len(database.GetNearCopiesKept(g.Id)), 0)

	test.IsEqualInt(t, keep(fx.creator, fx.b.Id), http.StatusOK)
	test.IsEqualBool(t, database.GetNearCopiesKept(g.Id)[fx.b.Id], true)
	// The creator keeps a pair she did not upload, and the admin one too
	test.IsEqualInt(t, keep(fx.creator, fx.a.Id), http.StatusOK)
	test.IsEqualBool(t, database.GetNearCopiesKept(g.Id)[fx.a.Id], true)
	test.IsEqualInt(t, keep(fx.admin, fx.c.Id), http.StatusOK)
	test.IsEqualBool(t, database.GetNearCopiesKept(g.Id)[fx.c.Id], true)
	// Settled for good: neither side is asked about the pair again
	for _, u := range []models.User{fx.creator, fx.member} {
		body := groupPageCall(showGroup, "GET", "/g?id="+g.Id, u, "").Body.String()
		test.IsEqualBool(t, strings.Contains(body, "data-near-file="), false)
	}
}
