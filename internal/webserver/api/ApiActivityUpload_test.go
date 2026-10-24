package api

import (
	"net/http"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

func uploadRowsNamed(name string) []models.Activity {
	var result []models.Activity
	for _, a := range database.GetActivitySince(0, 100000) {
		if a.Kind == models.ActivityUpload && a.FileName == name {
			result = append(result, a)
		}
	}
	return result
}

// The chunk-complete upload is recorded once, with its group; a duplicate
// that attachToGroup deletes records nothing (M6)
func TestChunkCompleteRecordsUpload(t *testing.T) {
	// The package's own fixture users: creating new ones would move the users
	// table's autoincrement, which TestUserCreate asserts, so this test does
	// not depend on running after it
	creator, found := database.GetUser(idAdmin)
	test.IsEqualBool(t, found, true)
	member, found := database.GetUser(idUser)
	test.IsEqualBool(t, found, true)
	creatorKey := groupUploadKey(t, creator)
	memberKey := groupUploadKey(t, member)
	// Leave the shared fixtures as they were: other tests count keys and files
	defer func() {
		for _, f := range groupUploadFilesNamed("act-member.jpg") {
			database.DeleteMetaData(f.Id)
		}
		database.DeleteApiKey(creatorKey.Id)
		database.DeleteApiKey(memberKey.Id)
	}()
	g, err := storage.CreateGroup("Activity target", 30, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	_, err = storage.JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)

	const content = "activity bytes uploaded into a group"
	w := completeIntoGroup(t, memberKey, "actmember", "act-member.jpg", content, g.Id)
	test.IsEqualInt(t, w.Code, http.StatusOK)
	rows := uploadRowsNamed("act-member.jpg")
	test.IsEqualInt(t, len(rows), 1)
	test.IsEqualBool(t, rows[0].ActorId == member.Id && rows[0].OwnerId == member.Id, true)
	test.IsEqualBool(t, rows[0].GroupId == g.Id && rows[0].GroupName == "Activity target", true)
	test.IsEqualString(t, rows[0].FileKind, "image")

	w = completeIntoGroup(t, creatorKey, "actduplicate", "act-duplicate.jpg", content, g.Id)
	test.IsEqualInt(t, w.Code, http.StatusConflict)
	test.IsEqualInt(t, len(uploadRowsNamed("act-duplicate.jpg")), 0)
}
