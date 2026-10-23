//go:build test

package storage

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/downloadlog"
	"github.com/forceu/gokapi/internal/test"
)

func activityRows(match func(models.Activity) bool) []models.Activity {
	var result []models.Activity
	for _, a := range database.GetActivitySince(0, 100000) {
		if match(a) {
			result = append(result, a)
		}
	}
	return result
}

// recordDownload writes an activity row exactly when it writes a Downloads
// row: HEAD and untagged requests record nothing (Part 4, A)
func TestRecordDownloadActivity(t *testing.T) {
	f := groupFile("actdl1", 7, nil)
	defer dropGroupFiles(f)
	ofFile := func(a models.Activity) bool { return a.Kind == models.ActivityDownload && a.FileId == f.Id }

	recordDownload(f, httptest.NewRequest(http.MethodGet, "/x", nil))
	recordDownload(f, downloadlog.With(httptest.NewRequest(http.MethodHead, "/x", nil), models.DownloadViaFile, 8))
	test.IsEqualInt(t, len(activityRows(ofFile)), 0)

	recordDownload(f, downloadlog.With(httptest.NewRequest(http.MethodGet, "/x", nil), models.DownloadViaZip, 8))
	rows := activityRows(ofFile)
	test.IsEqualInt(t, len(rows), 1)
	test.IsEqualBool(t, rows[0].ActorId == 8 && rows[0].OwnerId == 7 && rows[0].Via == models.DownloadViaZip, true)
	test.IsEqualBool(t, rows[0].FileName == f.Name && rows[0].FileKind == "image", true)

	recordDownload(f, downloadlog.With(httptest.NewRequest(http.MethodGet, "/x", nil), models.DownloadViaFile, 0))
	rows = activityRows(ofFile)
	test.IsEqualBool(t, len(rows) == 2 && rows[1].ActorId == 0, true)
}

// Creating a group and joining it are recorded; joining again is not
func TestGroupActivityRecorded(t *testing.T) {
	creator := models.User{Id: 71, Name: "creator@example.com", UserLevel: models.UserLevelUser}
	member := models.User{Id: 72, Name: "member@example.com", UserLevel: models.UserLevelUser}
	g, err := CreateGroup("Activity trip", 30, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	ofGroup := func(kind string) func(models.Activity) bool {
		return func(a models.Activity) bool { return a.Kind == kind && a.GroupId == g.Id }
	}
	created := activityRows(ofGroup(models.ActivityGroupCreated))
	test.IsEqualInt(t, len(created), 1)
	test.IsEqualBool(t, created[0].ActorId == creator.Id && created[0].GroupName == "Activity trip", true)

	_, err = JoinGroupByCode(g.JoinCode, member)
	test.IsNil(t, err)
	_, err = JoinGroupByCode(g.JoinCode, member)
	test.IsNil(t, err)
	_, err = JoinGroupByToken(g.InviteToken, creator) // already a member
	test.IsNil(t, err)
	joined := activityRows(ofGroup(models.ActivityGroupJoined))
	test.IsEqualInt(t, len(joined), 1)
	test.IsEqualInt(t, joined[0].ActorId, member.Id)
}

// A collection whose files are all refused leaves no group behind (M4), so
// it must not record a group_created either; a conversion that works records one
func TestGroupFromCollectionActivity(t *testing.T) {
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	createdNamed := func(name string) []models.Activity {
		return activityRows(func(a models.Activity) bool { return a.Kind == models.ActivityGroupCreated && a.GroupName == name })
	}
	refused := groupFile("actcolrefused", 7, func(f *models.File) { f.Encryption.IsEndToEndEncrypted = true })
	defer dropGroupFiles(refused)
	bad := models.Collection{Id: "actcolbad", Name: "Activity refused", UserId: 7, CreationDate: time.Now().Unix(), FileIds: []string{refused.Id}}
	database.SaveCollection(bad)
	defer database.DeleteCollection(bad.Id)
	_, _, err := GroupFromCollection(bad.Id, 60, FromCollectionOptions{}, owner)
	test.IsEqualBool(t, errors.Is(err, ErrGroupNothingAdded), true)
	test.IsEqualInt(t, len(createdNamed("Activity refused")), 0)

	ok := groupFile("actcolok", 7, nil)
	defer dropGroupFiles(ok)
	good := models.Collection{Id: "actcolgood", Name: "Activity converted", UserId: 7, CreationDate: time.Now().Unix(), FileIds: []string{ok.Id}}
	database.SaveCollection(good)
	defer database.DeleteCollection(good.Id)
	g, _, err := GroupFromCollection(good.Id, 60, FromCollectionOptions{}, owner)
	test.IsNil(t, err)
	defer database.DeleteCollectionGroup(good.Id)
	defer database.DeleteGroup(g.Id)
	rows := createdNamed("Activity converted")
	test.IsEqualInt(t, len(rows), 1)
	test.IsEqualBool(t, rows[0].GroupId == g.Id && rows[0].ActorId == 7, true)
}

// A zip renames clashing entries to "X (1).ext"; the activity row keeps the
// file's real name
func TestZipDownloadRecordsRealName(t *testing.T) {
	a := groupFile("actzipa", 7, func(f *models.File) { f.Name = "same.jpg" })
	b := groupFile("actzipb", 7, func(f *models.File) { f.Name = "same.jpg"; f.SHA1 = "sha-actzipb2" })
	defer dropGroupFiles(a, b)
	r := downloadlog.With(httptest.NewRequest(http.MethodGet, "/x", nil), models.DownloadViaZip, 8)
	ServeFilesAsZip([]models.File{a, b}, "test", httptest.NewRecorder(), r)
	rows := activityRows(func(x models.Activity) bool {
		return x.Kind == models.ActivityDownload && (x.FileId == a.Id || x.FileId == b.Id)
	})
	test.IsEqualInt(t, len(rows), 2)
	for _, row := range rows {
		test.IsEqualString(t, row.FileName, "same.jpg")
	}
}
