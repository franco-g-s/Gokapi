//go:build test

package activity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
)

func TestMain(m *testing.M) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	code := m.Run()
	testconfiguration.Delete()
	os.Exit(code)
}

func clearActivity(t *testing.T) {
	t.Helper()
	database.PruneActivity(1 << 62)
}

func allRows(t *testing.T) []models.Activity {
	t.Helper()
	return database.GetActivitySince(0, 100000)
}

func countKind(t *testing.T, kind string) int {
	t.Helper()
	n := 0
	for _, a := range allRows(t) {
		if a.Kind == kind {
			n++
		}
	}
	return n
}

func lastRow(t *testing.T) models.Activity {
	t.Helper()
	rows := allRows(t)
	if len(rows) == 0 {
		t.Fatal("no activity rows")
	}
	return rows[len(rows)-1]
}

func TestFeedStartFromNowAndReset(t *testing.T) {
	clearActivity(t)
	Upload(models.File{Id: "a1", Name: "x.jpg", SizeBytes: 10, ContentType: "image/jpeg", UserId: 7}, models.User{Id: 7})
	f := Feed(nil, 500) // no since header
	test.IsEqualInt(t, len(f.Events), 0)
	test.IsEqualBool(t, f.Next > 0 && !f.Reset, true)
	since := f.Next
	Upload(models.File{Id: "a2", Name: "y.mp4", SizeBytes: 20, ContentType: "video/mp4", UserId: 7}, models.User{Id: 7})
	f = Feed(&since, 500)
	test.IsEqualInt(t, len(f.Events), 1)
	test.IsEqualString(t, f.Events[0].File.Kind, "video")
	test.IsEqualBool(t, !f.Reset && !f.More && f.Next == since+1, true)
	future := f.Next + 100
	f = Feed(&future, 500)
	test.IsEqualBool(t, f.Reset && len(f.Events) == 0, true)
	test.IsEqualBool(t, f.Next == since+1, true)
}

func TestFeedResetWhenPruned(t *testing.T) {
	clearActivity(t)
	for i := 0; i < 4; i++ {
		Download(models.File{Id: fmt.Sprintf("p%d", i), UserId: 7}, 0, "file")
	}
	rows := allRows(t)
	oldest := rows[0].Id
	// since = oldest-1 is the cursor of someone who has seen nothing yet: fine
	justBefore := oldest - 1
	f := Feed(&justBefore, 500)
	test.IsEqualBool(t, !f.Reset && len(f.Events) == 4, true)
	// since = oldest-2: the row after it was pruned, so events were lost
	pruned := oldest - 2
	f = Feed(&pruned, 500)
	test.IsEqualBool(t, f.Reset && len(f.Events) == 0 && f.Next == rows[3].Id, true)
	// since: 0 means "from the oldest retained row", never a reset
	zero := int64(0)
	f = Feed(&zero, 500)
	test.IsEqualBool(t, !f.Reset && len(f.Events) == 4, true)

	// Everything pruned: the newest id survives, so the cursor is still valid
	clearActivity(t)
	last := rows[3].Id
	f = Feed(&last, 500)
	test.IsEqualBool(t, !f.Reset && len(f.Events) == 0 && f.Next == last, true)
	f = Feed(nil, 500)
	test.IsEqualBool(t, f.Next == last, true)
}

func TestFeedPaging(t *testing.T) {
	clearActivity(t)
	zero := int64(0)
	for i := 0; i < 7; i++ {
		Download(models.File{Id: fmt.Sprintf("d%d", i), UserId: 7}, 0, "file")
	}
	f := Feed(&zero, 5)
	test.IsEqualBool(t, len(f.Events) == 5 && f.More, true)
	f = Feed(&f.Next, 5)
	test.IsEqualBool(t, len(f.Events) == 2 && !f.More, true)
	test.IsEqualInt(t, f.Events[0].Actor, 0) // "someone"
	test.IsEqualInt(t, f.Events[0].Owner, 7)
	test.IsEqualString(t, f.Events[0].Via, "file")
	// limit is clamped to 1..1000
	f = Feed(&zero, 0)
	test.IsEqualBool(t, len(f.Events) == 1 && f.More, true)
	f = Feed(&zero, 5000)
	test.IsEqualBool(t, len(f.Events) == 7 && !f.More, true)
}

func TestFeedJsonShape(t *testing.T) {
	clearActivity(t)
	SignInBlocked("x@evil.example")
	zero := int64(0)
	out, err := json.Marshal(Feed(&zero, 500))
	test.IsNil(t, err)
	var parsed map[string]any
	test.IsNil(t, json.Unmarshal(out, &parsed))
	for _, key := range []string{"Result", "events", "next", "more", "reset", "users"} {
		_, ok := parsed[key]
		test.IsEqualBool(t, ok, true)
	}
	test.IsEqualString(t, parsed["Result"].(string), "OK")
	ev := parsed["events"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "at", "kind", "actor", "owner", "file", "group", "via", "detail"} {
		_, ok := ev[key]
		test.IsEqualBool(t, ok, true)
	}
	test.IsEqualBool(t, ev["file"] == nil && ev["group"] == nil, true)
	test.IsEqualString(t, ev["kind"].(string), "signin_blocked")
	test.IsEqualString(t, ev["detail"].(string), "x@evil.example")
	test.IsEqualBool(t, ev["actor"].(float64) == 0, true)

	// Empty lists are [], never null
	clearActivity(t)
	out, err = json.Marshal(Feed(nil, 500))
	test.IsNil(t, err)
	parsed = map[string]any{}
	test.IsNil(t, json.Unmarshal(out, &parsed))
	_, isList := parsed["events"].([]any)
	test.IsEqualBool(t, isList, true)
}

func TestFeedUsers(t *testing.T) {
	database.SaveUser(models.User{Id: 7, Name: "anna@example.com", UserLevel: models.UserLevelUser}, false)
	clearActivity(t)
	f := Feed(nil, 500)
	found := false
	for _, u := range f.Users {
		if u.Id == 7 {
			found = true
			test.IsEqualString(t, u.Email, "anna@example.com")
			test.IsEqualString(t, u.Name, "anna") // people.Name
			test.IsEqualBool(t, u.Admin, false)
			test.IsEqualBool(t, u.EmailDownloads, false) // stub until S1
		}
	}
	test.IsEqualBool(t, found, true)
	out, _ := json.Marshal(f.Users[0])
	var parsed map[string]any
	test.IsNil(t, json.Unmarshal(out, &parsed))
	for _, key := range []string{"id", "email", "name", "admin", "emailDownloads"} {
		_, ok := parsed[key]
		test.IsEqualBool(t, ok, true)
	}
}

func TestFileKind(t *testing.T) {
	cases := []struct{ contentType, name, want string }{
		{"image/png", "a.bin", "image"},
		{"application/octet-stream", "IMG_1.HEIC", "image"},
		{"", "raw.dng", "image"},
		{"video/quicktime", "a", "video"},
		{"application/octet-stream", "clip.MOV", "video"},
		{"application/pdf", "CV.pdf", "file"},
	}
	for _, c := range cases {
		test.IsEqualString(t, fileKind(c.contentType, c.name), c.want)
	}
}

func TestDiskWarningRearms(t *testing.T) {
	clearActivity(t)
	w := newDiskWatcher(20 << 30)
	w.check(15 << 30)
	w.check(14 << 30)
	w.check(25 << 30)
	w.check(19 << 30)
	test.IsEqualInt(t, countKind(t, "warning_disk"), 2)
	test.IsEqualString(t, lastRow(t).Detail, fmt.Sprint(uint64(19<<30)))
	test.IsEqualInt(t, lastRow(t).ActorId, 0)
}

func TestDownloadGroupFromFile(t *testing.T) {
	clearActivity(t)
	database.SaveGroup(models.Group{Id: "g1", Name: "Iceland trip", CreatorId: 7, Created: 1, JoinCode: "ACT-AA1", InviteToken: "acttoken1"})
	database.AddGroupFile("g1", "gf1")
	defer database.RemoveFileFromGroups("gf1")
	Download(models.File{Id: "gf1", Name: "IMG_0001.HEIC", SizeBytes: 99, UserId: 7}, 8, models.DownloadViaGroup)
	zero := int64(0)
	f := Feed(&zero, 500)
	test.IsEqualInt(t, len(f.Events), 1)
	e := f.Events[0]
	test.IsEqualString(t, e.Kind, "download")
	test.IsEqualBool(t, e.Group != nil && e.Group.Id == "g1" && e.Group.Name == "Iceland trip", true)
	test.IsEqualBool(t, e.File != nil && e.File.Id == "gf1" && e.File.Name == "IMG_0001.HEIC" && e.File.Size == 99 && e.File.Kind == "image", true)
	test.IsEqualBool(t, e.Actor == 8 && e.Owner == 7 && e.Via == "group", true)
}

func TestUploadActor(t *testing.T) {
	clearActivity(t)
	Upload(models.File{Id: "u1", Name: "a.pdf", UserId: 7}, models.User{Id: 7})
	test.IsEqualBool(t, lastRow(t).ActorId == 7 && lastRow(t).OwnerId == 7 && lastRow(t).Kind == "upload", true)
	// A file-request upload is a stranger's: the passed user is the request owner
	Upload(models.File{Id: "u2", Name: "b.pdf", UserId: 7, UploadRequestId: "fr1"}, models.User{Id: 7})
	test.IsEqualBool(t, lastRow(t).ActorId == 0 && lastRow(t).OwnerId == 7, true)
}

func TestGroupEvents(t *testing.T) {
	clearActivity(t)
	g := models.Group{Id: "g9", Name: "Wedding"}
	GroupCreated(g, models.User{Id: 7})
	GroupJoined(g, models.User{Id: 8})
	rows := allRows(t)
	test.IsEqualInt(t, len(rows), 2)
	test.IsEqualBool(t, rows[0].Kind == "group_created" && rows[0].ActorId == 7 && rows[0].GroupName == "Wedding" && rows[0].FileId == "", true)
	test.IsEqualBool(t, rows[1].Kind == "group_joined" && rows[1].ActorId == 8 && rows[1].GroupId == "g9", true)
}

func TestOnSignIn(t *testing.T) {
	clearActivity(t)
	OnSignIn(models.User{Id: 12, Name: "new@example.com"}, map[string]any{}, false)
	test.IsEqualInt(t, len(allRows(t)), 0)
	OnSignIn(models.User{Id: 12, Name: "new@example.com"}, map[string]any{}, true)
	test.IsEqualInt(t, countKind(t, "first_signin"), 1)
	test.IsEqualInt(t, lastRow(t).ActorId, 12)
}

func TestViewAsEvents(t *testing.T) {
	clearActivity(t)
	ViewAs("viewas_start", models.User{Id: 1}, models.User{Id: 12}, "")
	ViewAs("viewas_end", models.User{Id: 1}, models.User{Id: 12}, "exit")
	rows := allRows(t)
	test.IsEqualInt(t, len(rows), 2)
	test.IsEqualBool(t, rows[0].Kind == "viewas_start" && rows[0].ActorId == 1 && rows[0].Detail == "target=12", true)
	test.IsEqualBool(t, rows[1].Kind == "viewas_end" && rows[1].Detail == "target=12 reason=exit", true)
}

func TestPreviewFailed(t *testing.T) {
	clearActivity(t)
	PreviewFailed(models.File{Id: "pv1", Name: "broken.heic", UserId: 7, SizeBytes: 5})
	r := lastRow(t)
	test.IsEqualBool(t, r.Kind == "warning_preview" && r.FileId == "pv1" && r.OwnerId == 7 && r.ActorId == 0 && r.FileKind == "image", true)
}

func TestFeedResetWhenAllPruned(t *testing.T) {
	clearActivity(t)
	Download(models.File{Id: "ap1", UserId: 7}, 0, "file")
	Download(models.File{Id: "ap2", UserId: 7}, 0, "file")
	rows := allRows(t)
	behind := rows[0].Id // has seen ap1, not ap2
	clearActivity(t)     // ap2 is pruned before it was read
	f := Feed(&behind, 500)
	test.IsEqualBool(t, f.Reset && len(f.Events) == 0 && f.Next == rows[1].Id, true)
}
