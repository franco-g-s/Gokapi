//go:build !integration && test

package webserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/downloadPasswordToken"
)

// feedCall runs a handler as this user (as requireLogin would) and, when
// token is set, with that session cookie, which the collection checks read
func feedCall(handler http.HandlerFunc, method, target string, user *models.User, token string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	if user != nil {
		r = authentication.SetUserInRequest(r, *user)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) listAnswer {
	t.Helper()
	test.IsEqualInt(t, w.Code, http.StatusOK)
	var answer listAnswer
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &answer))
	test.IsEqualString(t, answer.Result, "OK")
	return answer
}

func feedIds(answer listAnswer) []string {
	ids := make([]string, 0, len(answer.Items))
	for _, item := range answer.Items {
		ids = append(ids, item.Id)
	}
	return ids
}

func feedSession(token string, userId int) {
	database.SaveSession(token, models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: userId})
}

// bigGroup is a group of n files: the creator's even ones, the member's odd
// ones, uploaded at i (no previews, so shooting order is upload order)
type bigGroup struct {
	group                    models.Group
	creator, member, outside models.User
	files                    []models.File
}

func newBigGroup(t *testing.T, n int) bigGroup {
	t.Helper()
	bg := bigGroup{
		creator: models.User{Id: 191, Name: "carla.list@example.com", UserLevel: models.UserLevelUser},
		member:  models.User{Id: 192, Name: "anna.list@example.com", UserLevel: models.UserLevelUser},
		outside: models.User{Id: 193, Name: "zed.list@example.com", UserLevel: models.UserLevelUser},
	}
	for _, u := range []models.User{bg.creator, bg.member, bg.outside} {
		database.SaveUser(u, false)
	}
	feedSession("listcreator", bg.creator.Id)
	feedSession("listmember", bg.member.Id)
	feedSession("listoutside", bg.outside.Id)
	g, err := storage.CreateGroup("List trip", 30, bg.creator)
	test.IsNil(t, err)
	_, err = storage.JoinGroupByToken(g.InviteToken, bg.member)
	test.IsNil(t, err)
	bg.group = g
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		owner := bg.creator.Id
		if i%2 == 1 {
			owner = bg.member.Id
		}
		f := accessFile(fmt.Sprintf("p3list%04d", i), func(f *models.File) {
			f.SHA1 = fmt.Sprintf("p3listsha%04d", i)
			f.UserId = owner
			f.UploadDate = int64(1000 + i)
			f.Name = fmt.Sprintf("list-%04d.jpg", i)
		})
		bg.files = append(bg.files, f)
		ids = append(ids, f.Id)
	}
	added, err := storage.AddFilesToGroup(g, ids, models.User{Id: 5, UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll})
	test.IsNil(t, err)
	test.IsEqualInt(t, len(added.Added), n)
	t.Cleanup(func() {
		database.DeleteGroup(g.Id)
		for _, f := range bg.files {
			database.DeleteDownloadsOfFile(f.Id)
			database.RemoveFileFromGroups(f.Id)
			database.DeleteMetaData(f.Id)
		}
		for _, u := range []models.User{bg.creator, bg.member, bg.outside} {
			database.DeleteUser(u.Id)
		}
		for _, token := range []string{"listcreator", "listmember", "listoutside"} {
			database.DeleteSession(token)
		}
	})
	return bg
}

var groupTileRe = regexp.MustCompile(`<figure class="fgs-tile" data-mine="(true|false)" data-id="p3list`)

func TestPagesRenderFirstBatch(t *testing.T) {
	bg := newBigGroup(t, 130)
	r := authentication.SetUserInRequest(httptest.NewRequest("GET", "/g?id="+bg.group.Id, nil), bg.member)
	source, files, _ := groupSource(r, bg.group, bg.member)
	test.IsEqualInt(t, len(source.Items), 130)
	test.IsEqualInt(t, len(files), 130)
	b := newFileBrowser("fgs-group", ctxGroup, nil, bg.member)
	b.WithGroup(bg.group, nil)
	b.WithList(source, "./gl?id="+bg.group.Id+"&")
	test.IsEqualInt(t, b.Total, 130)
	test.IsEqualInt(t, b.All, 130)
	test.IsEqualInt(t, b.New, 65) // the creator's, none downloaded
	test.IsEqualInt(t, len(b.Items), 100)
	test.IsNotEmpty(t, b.Next)
	test.IsEqualString(t, b.Sort, "taken")
	test.IsEqualBool(t, b.Desc, false)
	test.IsEqualString(t, b.ListUrl, "./gl?id="+bg.group.Id+"&")
	test.IsEqualString(t, b.Items[0].File.Id, "p3list0000")
	test.IsEqualString(t, b.Items[99].File.Id, "p3list0099")

	// The page itself renders exactly the first 100 tiles
	w := groupPageCall(showGroup, "GET", "/g?id="+bg.group.Id, bg.member, "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualInt(t, len(groupTileRe.FindAllString(w.Body.String(), -1)), 100)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), `data-id="p3list0099"`), true)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), `data-id="p3list0100"`), false)
}

func TestGroupFeedSecondBatch(t *testing.T) {
	bg := newBigGroup(t, 130)
	first := decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id, &bg.member, ""))
	test.IsEqualInt(t, first.Total, 130)
	test.IsEqualInt(t, len(first.Items), 100)
	test.IsNotEmpty(t, first.Next)

	// The page's own cursor and the feed's first answer agree
	r := authentication.SetUserInRequest(httptest.NewRequest("GET", "/g", nil), bg.member)
	source, _, _ := groupSource(r, bg.group, bg.member)
	b := newFileBrowser("fgs-group", ctxGroup, nil, bg.member)
	b.WithList(source, "")
	test.IsEqualString(t, b.Next, first.Next)

	second := decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id+"&after="+b.Next, &bg.member, ""))
	test.IsEqualInt(t, second.Total, 130)
	test.IsEqualInt(t, second.All, 130)
	test.IsEqualString(t, second.Next, "")
	want := make([]string, 0, 30)
	for i := 100; i < 130; i++ {
		want = append(want, fmt.Sprintf("p3list%04d", i))
	}
	if !slices.Equal(feedIds(second), want) {
		t.Fatalf("second batch %v", feedIds(second))
	}
	// Filters and a bad query
	mine := decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id+"&person="+strconv.Itoa(bg.member.Id)+"&q=LIST-001", &bg.member, ""))
	test.IsEqualString(t, strings.Join(feedIds(mine), ","), "p3list0011,p3list0013,p3list0015,p3list0017,p3list0019")
	w := feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id+"&sort=bogus", &bg.member, "")
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
	w = feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id+"&sort=size&after="+b.Next, &bg.member, "")
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
	// Type (R1): every file is a .jpg without a preview
	photos := decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id+"&kind=image", &bg.member, ""))
	test.IsEqualInt(t, photos.Total, 130)
	test.IsEqualInt(t, photos.All, 130)
	videos := decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id+"&kind=video", &bg.member, ""))
	test.IsEqualInt(t, videos.Total, 0)
	test.IsEqualInt(t, videos.All, 130)
	test.IsEqualInt(t, len(videos.Items), 0)
	w = feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id+"&kind=bogus", &bg.member, "")
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
}

// collectionFixture is a collection of two of user 8's files, with a password
type collectionFixture struct {
	collection models.Collection
	one, two   models.File
}

func newCollectionFixture(t *testing.T, password string) collectionFixture {
	t.Helper()
	collectionOwnerUsers()
	one := accessFile("p3listcol00001", func(f *models.File) { f.Name = "col-one.jpg"; f.UserId = 8; f.SHA1 = "p3listcolsha1"; f.UploadDate = 10 })
	two := accessFile("p3listcol00002", func(f *models.File) { f.Name = "col-two.jpg"; f.UserId = 8; f.SHA1 = "p3listcolsha2"; f.UploadDate = 20 })
	c, err := storage.CreateCollection(storage.CollectionParameters{Name: "List col", FileIds: []string{one.Id, two.Id}, Password: password}, models.User{Id: 8})
	test.IsNil(t, err)
	t.Cleanup(func() {
		database.DeleteCollectionGroup(c.Id)
		database.DeleteCollection(c.Id)
		database.DeleteMetaData(one.Id)
		database.DeleteMetaData(two.Id)
	})
	return collectionFixture{collection: c, one: one, two: two}
}

func TestFeedAccessMatchesPage(t *testing.T) {
	bg := newBigGroup(t, 2)
	// /gl: a non-member is told the group does not exist, as by /g
	test.IsEqualInt(t, groupPageCall(showGroup, "GET", "/g?id="+bg.group.Id, bg.outside, "").Code, http.StatusNotFound)
	test.IsEqualInt(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id, &bg.outside, "").Code, http.StatusNotFound)
	test.IsEqualInt(t, feedCall(groupListFeed, "GET", "/gl?id=nosuchgroup", &bg.member, "").Code, http.StatusNotFound)
	// Signed out, every login feed answers 401 (requireLogin)
	for _, path := range []string{"/gl?id=" + bg.group.Id, "/files/list?src=files", "/files/replacecandidates"} {
		code, _ := filesGet(t, "", path)
		test.IsEqualInt(t, code, http.StatusUnauthorized)
	}

	// /cl: the password gate, as on /c
	fx := newCollectionFixture(t, "secret")
	cl := "/cl?id=" + fx.collection.Id
	test.IsEqualInt(t, feedCall(collectionListFeed, "GET", cl, nil, "").Code, http.StatusNotFound)
	wrong := &http.Cookie{Name: "pc" + fx.collection.Id, Value: "wrong"}
	test.IsEqualInt(t, feedCall(collectionListFeed, "GET", cl, nil, "", wrong).Code, http.StatusNotFound)
	test.IsEqualBool(t, strings.Contains(openCollection(fx.collection.Id, "", wrong), "col-one.jpg"), false)
	right := &http.Cookie{Name: "pc" + fx.collection.Id, Value: downloadPasswordToken.GenerateWithTtl(fx.collection.Id, time.Hour)}
	unlocked := decodeList(t, feedCall(collectionListFeed, "GET", cl, nil, "", right))
	test.IsEqualInt(t, unlocked.Total, 2)
	// newest first (P9)
	test.IsEqualString(t, strings.Join(feedIds(unlocked), ","), fx.two.Id+","+fx.one.Id)
	test.IsEqualInt(t, feedCall(collectionListFeed, "GET", "/cl?id=nosuch", nil, "").Code, http.StatusNotFound)

	// src=collection: the owner and a PERM_LIST admin get the management
	// list, a signed-in stranger does not (the page gives them the public one)
	owner, _ := database.GetUser(8)
	admin, _ := database.GetUser(5)
	stranger, _ := database.GetUser(7)
	src := "/files/list?src=collection&id=" + fx.collection.Id
	managed := decodeList(t, feedCall(fileListFeed, "GET", src, &owner, "colowner8"))
	test.IsEqualInt(t, managed.Total, 2)
	test.IsEqualBool(t, managed.Items[0].CanEdit, true)
	decodeList(t, feedCall(fileListFeed, "GET", src, &admin, "colowner5"))
	test.IsEqualInt(t, feedCall(fileListFeed, "GET", src, &stranger, "validsession").Code, http.StatusNotFound)
	test.IsEqualBool(t, strings.Contains(openCollection(fx.collection.Id, "validsession", right), `data-context="collection-admin"`), false)

	// A closed collection: its group has gone. /c answers with the error
	// page, /cl with a 404, even with the password
	database.SaveCollectionGroup(models.CollectionGroup{CollectionId: fx.collection.Id, GroupId: "gonegroup", LinkMode: models.CollectionLinkInvite})
	test.IsEqualBool(t, strings.Contains(openCollection(fx.collection.Id, "", right), "col-one.jpg"), false)
	test.IsEqualInt(t, feedCall(collectionListFeed, "GET", cl, nil, "", right).Code, http.StatusNotFound)
	test.IsEqualInt(t, feedCall(fileListFeed, "GET", src, &owner, "colowner8").Code, http.StatusNotFound)
	database.DeleteCollectionGroup(fx.collection.Id)

	// src=request: only a request the viewer's File requests page lists
	anna := models.User{Id: 194, Name: "anna.req@example.com", UserLevel: models.UserLevelUser,
		Permissions: models.UserPermGuestUploads}
	database.SaveUser(anna, false)
	defer database.DeleteUser(anna.Id)
	request := models.FileRequest{Id: "p3listreq", UserId: 5, Name: "Admin's request", MaxFiles: 10, MaxSize: 10,
		Expiry: time.Now().Add(time.Hour).Unix(), CreationDate: 1}
	database.SaveFileRequest(request)
	defer database.DeleteFileRequest(request)
	upload := accessFile("p3listrequpload1", func(f *models.File) { f.UserId = 5; f.UploadRequestId = request.Id; f.SHA1 = "p3listreqsha" })
	defer database.DeleteMetaData(upload.Id)
	mineReq := decodeList(t, feedCall(fileListFeed, "GET", "/files/list?src=request&id="+request.Id, &admin, ""))
	test.IsEqualString(t, strings.Join(feedIds(mineReq), ","), upload.Id)
	test.IsEqualString(t, mineReq.Items[0].Download, "")
	test.IsEqualInt(t, feedCall(fileListFeed, "GET", "/files/list?src=request&id="+request.Id, &anna, "").Code, http.StatusNotFound)
	test.IsEqualInt(t, feedCall(fileListFeed, "GET", "/files/list?src=request&id=nosuch", &admin, "").Code, http.StatusNotFound)
	test.IsEqualInt(t, feedCall(fileListFeed, "GET", "/files/list?src=bogus", &admin, "").Code, http.StatusBadRequest)
}

func TestFeedForeignBlank(t *testing.T) {
	bg := newBigGroup(t, 2)
	answer := decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id, &bg.member, ""))
	test.IsEqualInt(t, len(answer.Items), 2)
	for _, item := range answer.Items {
		if item.UploaderId == bg.member.Id {
			test.IsEqualBool(t, item.Foreign, false)
			test.IsEqualBool(t, item.Mine, true)
			test.IsNotEmpty(t, item.UrlDownload)
			continue
		}
		test.IsEqualBool(t, item.Foreign, true)
		test.IsEqualString(t, item.UrlDownload, "")
		test.IsEqualString(t, item.HotlinkId, "")
		test.IsEqualString(t, item.UrlHotlink, "")
		test.IsEqualInt64(t, item.ExpireAt, 0)
		test.IsEqualBool(t, item.CanEdit, false)
		test.IsEqualString(t, item.UploaderName, "carla.list")
		test.IsEqualString(t, item.Download, "./gd?id="+bg.group.Id+"&file="+item.Id)
	}
	// The raw JSON carries no management value for the foreign item either
	raw := feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id, &bg.member, "").Body.String()
	test.IsEqualBool(t, strings.Contains(raw, "/d?id="+bg.files[0].Id), false)

	// A public collection carries none of the members' management data
	fx := newCollectionFixture(t, "")
	public := feedCall(collectionListFeed, "GET", "/cl?id="+fx.collection.Id, nil, "")
	answer = decodeList(t, public)
	for _, item := range answer.Items {
		test.IsEqualString(t, item.UrlDownload, "")
		test.IsEqualString(t, item.HotlinkId, "")
		test.IsEqualInt64(t, item.ExpireAt, 0)
		test.IsEqualInt(t, item.UploaderId, 0)
		test.IsEqualString(t, item.UploaderName, "")
		test.IsEqualBool(t, item.CanEdit || item.CanDelete || item.Mine, false)
		test.IsEqualString(t, item.Download, "./cd?id="+fx.collection.Id+"&file="+item.Id)
	}
	test.IsEqualBool(t, strings.Contains(public.Body.String(), "/d?id="), false)
}

func TestFeedNeverWrites(t *testing.T) {
	bg := newBigGroup(t, 3)
	fx := newCollectionFixture(t, "")
	downloaded := len(database.GetDownloadedFileIds(bg.member.Id))
	seen := len(database.GetCollectionsSeen(bg.member.Id))
	remaining, count := downloadState(t, fx.one.Id)
	decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+bg.group.Id, &bg.member, "listmember"))
	decodeList(t, feedCall(collectionListFeed, "GET", "/cl?id="+fx.collection.Id, nil, "listmember"))
	decodeList(t, feedCall(fileListFeed, "GET", "/files/list?src=files&scope=all", &bg.member, "listmember"))
	test.IsEqualInt(t, len(database.GetDownloadedFileIds(bg.member.Id)), downloaded)
	test.IsEqualInt(t, len(database.GetCollectionsSeen(bg.member.Id)), seen)
	r2, c2 := downloadState(t, fx.one.Id)
	test.IsEqualInt(t, r2, remaining)
	test.IsEqualInt(t, c2, count)
	test.IsEqualBool(t, sharedSeen(bg.member.Id, fx.collection.Id), false)
}

func TestFeedMethod(t *testing.T) {
	bg := newBigGroup(t, 1)
	fx := newCollectionFixture(t, "")
	for _, call := range []struct {
		handler http.HandlerFunc
		target  string
	}{
		{groupListFeed, "/gl?id=" + bg.group.Id},
		{collectionListFeed, "/cl?id=" + fx.collection.Id},
		{fileListFeed, "/files/list?src=files"},
		{replaceCandidates, "/files/replacecandidates"},
	} {
		for _, method := range []string{"POST", "PUT", "DELETE"} {
			w := feedCall(call.handler, method, call.target, &bg.member, "listmember")
			test.IsEqualInt(t, w.Code, http.StatusMethodNotAllowed)
			test.IsEqualString(t, w.Header().Get("Allow"), "GET")
		}
		// and every GET is uncached
		w := feedCall(call.handler, "GET", call.target, &bg.member, "listmember")
		test.IsEqualInt(t, w.Code, http.StatusOK)
		test.IsEqualString(t, w.Header().Get("cache-control"), "no-store, no-cache")
	}
}

func TestFilesFeedScope(t *testing.T) {
	bg := newBigGroup(t, 4)
	// the member holds no PERM_LIST: their own two, and the creator's two
	// through the group
	feed := "/files/list?src=files"
	def := decodeList(t, feedCall(fileListFeed, "GET", feed, &bg.member, ""))
	mine := decodeList(t, feedCall(fileListFeed, "GET", feed+"&scope=mine", &bg.member, ""))
	all := decodeList(t, feedCall(fileListFeed, "GET", feed+"&scope=all", &bg.member, ""))
	test.IsEqualString(t, strings.Join(feedIds(def), ","), "p3list0003,p3list0001")
	test.IsEqualString(t, strings.Join(feedIds(mine), ","), "p3list0003,p3list0001")
	test.IsEqualString(t, strings.Join(feedIds(all), ","), "p3list0003,p3list0002,p3list0001,p3list0000")
	test.IsEqualInt(t, all.All, 4)
	test.IsEqualInt(t, mine.All, 4)
	for _, item := range all.Items {
		if item.Mine {
			test.IsEqualBool(t, item.Foreign, false)
			test.IsEqualString(t, item.Download, "./files/download?id="+item.Id)
			test.IsEqualInt(t, item.UploaderId, bg.member.Id)
			continue
		}
		test.IsEqualInt(t, item.UploaderId, bg.creator.Id)
		test.IsEqualBool(t, item.Foreign, true)
		test.IsEqualString(t, item.UrlDownload, "")
		test.IsEqualString(t, item.HotlinkId, "")
		test.IsEqualInt64(t, item.ExpireAt, 0)
		test.IsEqualBool(t, item.CanEdit || item.CanDelete || item.CanReplace || item.CanRemove, false)
	}
	// The page's first batch is the default query: mine only
	r := authentication.SetUserInRequest(httptest.NewRequest("GET", "/files", nil), bg.member)
	source, meta := filesTabSource(r, bg.member)
	test.IsEqualBool(t, meta.InGroups, true)
	test.IsEqualBool(t, source.HasScope, true)
	test.IsEqualString(t, source.DefaultScope, "mine")
	page := groupPageCall(showFiles, "GET", "/files", bg.member, "").Body.String()
	test.IsEqualBool(t, strings.Contains(page, `data-id="p3list0001"`), true)
	test.IsEqualBool(t, strings.Contains(page, `data-id="p3list0000"`), false)
	// The outsider is in no group: no switch, only their own
	_, meta = filesTabSource(r, bg.outside)
	test.IsEqualBool(t, meta.InGroups, false)
	test.IsEqualInt(t, len(decodeList(t, feedCall(fileListFeed, "GET", feed+"&scope=all", &bg.outside, "")).Items), 0)
}

func TestReplaceCandidates(t *testing.T) {
	ben := models.User{Id: 195, Name: "ben.replace@example.com", UserLevel: models.UserLevelUser}
	database.SaveUser(ben, false)
	defer database.DeleteUser(ben.Id)
	mk := func(id, name string, mutate func(f *models.File)) models.File {
		f := accessFile(id, func(f *models.File) {
			f.UserId = ben.Id
			f.Name = name
			f.SHA1 = id + "sha"
			if mutate != nil {
				mutate(f)
			}
		})
		t.Cleanup(func() { database.DeleteMetaData(f.Id) })
		return f
	}
	zeta := mk("p3listrepl001", "Zeta.jpg", nil)
	alpha := mk("p3listrepl002", "alpha.jpg", nil)
	e2e := mk("p3listrepl003", "e2e.jpg", func(f *models.File) { f.Encryption.IsEndToEndEncrypted = true; f.Encryption.IsEncrypted = true })
	req := mk("p3listrepl004", "req.jpg", func(f *models.File) { f.UploadRequestId = "p3listsomereq" })
	expired := mk("p3listrepl005", "expired.jpg", func(f *models.File) { f.ExpireAt = 1 })
	others := accessFile("p3listrepl006", func(f *models.File) { f.Name = "others.jpg"; f.SHA1 = "p3listrepl006sha" })
	defer database.DeleteMetaData(others.Id)

	type candidate struct{ Id, Name string }
	candidates := func(user models.User) []candidate {
		w := feedCall(replaceCandidates, "GET", "/files/replacecandidates", &user, "")
		test.IsEqualInt(t, w.Code, http.StatusOK)
		var result []candidate
		test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result
	}
	got := candidates(ben)
	test.IsEqualInt(t, len(got), 2)
	test.IsEqualString(t, got[0].Id, alpha.Id)
	test.IsEqualString(t, got[0].Name, "alpha.jpg")
	test.IsEqualString(t, got[1].Id, zeta.Id)

	admin, _ := database.GetUser(5)
	got = candidates(admin)
	ids := make([]string, 0, len(got))
	for _, c := range got {
		ids = append(ids, c.Id)
	}
	test.IsEqualBool(t, slices.Contains(ids, alpha.Id) && slices.Contains(ids, zeta.Id) && slices.Contains(ids, others.Id), true)
	for _, gone := range []string{e2e.Id, req.Id, expired.Id} {
		test.IsEqualBool(t, slices.Contains(ids, gone), false)
	}
	test.IsEqualBool(t, slices.IsSortedFunc(got, func(a, b candidate) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}), true)
}

func postForm(handler http.HandlerFunc, target string, user models.User, form string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler(w, authentication.SetUserInRequest(r, user))
	return w
}

func TestZipPostIds(t *testing.T) {
	bg := newBigGroup(t, 3)
	a, b := bg.files[0], bg.files[1]
	w := postForm(downloadGroupZip, "/gz?id="+bg.group.Id, bg.member, "files="+a.Id+","+b.Id)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	names := zipEntries(t, w.Body.Bytes())
	slices.Sort(names)
	test.IsEqualString(t, strings.Join(names, ","), a.Name+","+b.Name)
	// still counted and recorded, exactly as by GET
	test.IsEqualInt(t, downloadRows(t, bg.member.Id, models.DownloadViaZip, a.Id, b.Id), 2)

	admin, _ := database.GetUser(5)
	w = postForm(downloadFilesZip, "/files/zip", admin, "ids="+a.Id+","+b.Id)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsEqualInt(t, len(zipEntries(t, w.Body.Bytes())), 2)

	fx := newCollectionFixture(t, "")
	r := httptest.NewRequest("POST", "/cz?id="+fx.collection.Id, strings.NewReader("files="+fx.one.Id))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	downloadCollectionZip(rec, r)
	test.IsEqualInt(t, rec.Code, http.StatusOK)
	test.IsEqualString(t, strings.Join(zipEntries(t, rec.Body.Bytes()), ","), "col-one.jpg")
	// an empty selection is still nothing, not everything
	w = postForm(downloadGroupZip, "/gz?id="+bg.group.Id, bg.member, "files=")
	test.IsEqualInt(t, w.Code, http.StatusGone)

	// A POST whose body is not a urlencoded form, or lacks the field, is an
	// empty selection: 410, nothing counted or recorded, never every member
	before := downloadRows(t, bg.member.Id, models.DownloadViaZip, bg.files[0].Id, bg.files[1].Id, bg.files[2].Id)
	remaining, count := downloadState(t, fx.one.Id)
	odd := func(handler http.HandlerFunc, target, contentType, body string) int {
		r := httptest.NewRequest("POST", target, strings.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		handler(rec, authentication.SetUserInRequest(r, bg.member))
		return rec.Code
	}
	multipart := "--XB\r\nContent-Disposition: form-data; name=\"files\"\r\n\r\n" + a.Id + "\r\n--XB--\r\n"
	for _, target := range []string{"/gz?id=" + bg.group.Id, "/cz?id=" + fx.collection.Id} {
		handler := downloadGroupZip
		if strings.HasPrefix(target, "/cz") {
			handler = downloadCollectionZip
		}
		test.IsEqualInt(t, odd(handler, target, "multipart/form-data; boundary=XB", multipart), http.StatusGone)
		test.IsEqualInt(t, odd(handler, target, "application/json", `{"files":"x"}`), http.StatusGone)
		test.IsEqualInt(t, odd(handler, target, "", ""), http.StatusGone)
		test.IsEqualInt(t, odd(handler, target, "application/x-www-form-urlencoded", "other=1"), http.StatusGone)
		// the files= of the URL does not count for a POST either
		test.IsEqualInt(t, odd(handler, target+"&files="+a.Id, "application/x-www-form-urlencoded", ""), http.StatusGone)
	}
	test.IsEqualInt(t, downloadRows(t, bg.member.Id, models.DownloadViaZip, bg.files[0].Id, bg.files[1].Id, bg.files[2].Id), before)
	r2, c2 := downloadState(t, fx.one.Id)
	test.IsEqualInt(t, r2, remaining)
	test.IsEqualInt(t, c2, count)
	// /files/zip likewise; b is the member's own, so only the body decides
	test.IsEqualInt(t, odd(downloadFilesZip, "/files/zip?ids="+b.Id, "multipart/form-data; boundary=XB", strings.Replace(multipart, `name="files"`, `name="ids"`, 1)), http.StatusGone)
	test.IsEqualInt(t, odd(downloadFilesZip, "/files/zip", "application/x-www-form-urlencoded", "ids="+b.Id), http.StatusOK)
	// and a urlencoded POST still works after all that
	w = postForm(downloadGroupZip, "/gz?id="+bg.group.Id, bg.member, "files="+a.Id)
	test.IsEqualInt(t, w.Code, http.StatusOK)
}

var tileAttrRe = regexp.MustCompile(`\s(data-[a-z-]+)="([^"]*)"`)

func TestParityJsonVsTemplate(t *testing.T) {
	fx := newGroupPageFixture(t)
	// anna sees carla's B: foreign, with a ready preview
	r := authentication.SetUserInRequest(httptest.NewRequest("GET", "/g", nil), fx.member)
	source, _, _ := groupSource(r, fx.group, fx.member)
	var item browserItem
	for _, it := range source.Items {
		if it.File.Id == fx.b.Id {
			item = it
		}
	}
	test.IsEqualString(t, item.File.Id, fx.b.Id)
	item.Downloaded = true // so the tick renders as true on both sides
	b := newFileBrowser("fgs-group", ctxGroup, nil, fx.member)
	b.WithGroup(fx.group, nil)
	var buf bytes.Buffer
	test.IsNil(t, templateFolder.ExecuteTemplate(&buf, "file_browser_tile", b.TileAt(0, item)))
	tile := buf.String()
	tag := tile[:strings.Index(tile, ">")]
	attrs := map[string]string{}
	for _, m := range tileAttrRe.FindAllStringSubmatch(tag, -1) {
		attrs[m[1]] = html.UnescapeString(m[2])
	}
	thumb := regexp.MustCompile(`<img [^>]*src="([^"]*)"[^>]*style="background:([^"]*)"`).FindStringSubmatch(tile)
	test.IsEqualInt(t, len(thumb), 3)
	attrs["thumb"] = html.UnescapeString(thumb[1])
	attrs["color"] = html.UnescapeString(thumb[2])

	j := listItemOf(item)
	// round-trip through JSON, as the client receives it
	raw, err := json.Marshal(j)
	test.IsNil(t, err)
	var got listItemJson
	test.IsNil(t, json.Unmarshal(raw, &got))
	str := func(v any) string { return fmt.Sprint(v) }
	for attr, value := range map[string]string{
		"data-id":             got.Id,
		"data-name":           got.Name,
		"data-download":       got.Download,
		"data-bytes":          str(got.Bytes),
		"data-taken":          str(got.TakenAt),
		"data-uploaded":       str(got.UploadDate),
		"data-uploader":       got.UploaderName,
		"data-uploader-id":    str(got.UploaderId),
		"data-downloaded":     str(got.Downloaded),
		"data-mine":           str(got.Mine),
		"data-foreign":        str(got.Foreign),
		"data-kind":           got.Preview.Kind,
		"data-w":              str(got.Preview.Width),
		"data-h":              str(got.Preview.Height),
		"thumb":               got.Preview.Thumb,
		"data-view":           got.Preview.View,
		"data-video":          got.Preview.Video,
		"color":               got.Preview.Color,
		"data-preview-status": got.Preview.Status,
		"data-size":           got.Size,
	} {
		rendered, ok := attrs[attr]
		if !ok {
			t.Errorf("the tile renders no %s", attr)
			continue
		}
		if rendered != value {
			t.Errorf("%s: tile %q, JSON %q", attr, rendered, value)
		}
	}
	test.IsEqualBool(t, got.Foreign, true)
	test.IsNotEmpty(t, got.Preview.Thumb)
}
