//go:build test

package storage

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
