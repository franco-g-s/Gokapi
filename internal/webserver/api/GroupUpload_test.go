package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// groupUploadKey is an upload key for a user created for these tests
func groupUploadKey(t *testing.T, user models.User) models.ApiKey {
	t.Helper()
	database.SaveUser(user, false)
	key := generateNewKey(false, user.Id, "", "")
	key.GrantPermission(models.ApiPermUpload)
	database.SaveApiKey(key)
	return key
}

// completeIntoGroup writes a chunk with this content and calls chunk/complete
// for it with the group id and the extra headers
func completeIntoGroup(t *testing.T, key models.ApiKey, uuid, name, content, groupId string, extra ...test.Header) *httptest.ResponseRecorder {
	t.Helper()
	test.IsNil(t, os.WriteFile("test/data/chunk-"+uuid, []byte(content), 0600))
	headers := []test.Header{
		{Name: "apikey", Value: key.Id},
		{Name: "uuid", Value: uuid},
		{Name: "filename", Value: name},
		{Name: "filesize", Value: strconv.Itoa(len(content))},
		{Name: "groupId", Value: groupId},
	}
	headers = append(headers, extra...)
	w, r := test.GetRecorder("POST", "/api/chunk/complete", nil, headers, nil)
	Process(w, r)
	return w
}

// groupUploadFilesNamed returns the stored files with this name
func groupUploadFilesNamed(name string) []models.File {
	var result []models.File
	for _, f := range database.GetAllMetadata() {
		if f.Name == name {
			result = append(result, f)
		}
	}
	return result
}

func TestChunkCompleteGroup(t *testing.T) {
	creator := models.User{Id: 120, Name: "anna@example.com", UserLevel: models.UserLevelUser}
	member := models.User{Id: 121, Name: "bert@example.com", UserLevel: models.UserLevelUser}
	stranger := models.User{Id: 122, Name: "carl@example.com", UserLevel: models.UserLevelUser}
	creatorKey := groupUploadKey(t, creator)
	memberKey := groupUploadKey(t, member)
	strangerKey := groupUploadKey(t, stranger)

	g, err := storage.CreateGroup("Upload target", 30, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	_, err = storage.JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)

	// 1. A non-member: 403, and no file is created
	w := completeIntoGroup(t, strangerKey, "g7stranger", "g7-stranger.txt", "stranger content", g.Id)
	test.IsEqualInt(t, w.Code, http.StatusForbidden)
	test.IsEqualInt(t, len(groupUploadFilesNamed("g7-stranger.txt")), 0)
	test.IsEqualInt(t, len(database.GetGroupFileIds(g.Id)), 0)

	// An unknown group: 403 too
	w = completeIntoGroup(t, memberKey, "g7unknown", "g7-unknown.txt", "unknown content", "nosuchgroup")
	test.IsEqualInt(t, w.Code, http.StatusForbidden)
	test.IsEqualInt(t, len(groupUploadFilesNamed("g7-unknown.txt")), 0)

	// End-to-end encrypted files cannot go into a group
	w = completeIntoGroup(t, memberKey, "g7e2e", "g7-e2e.txt", "e2e content", g.Id,
		test.Header{Name: "isE2E", Value: "true"}, test.Header{Name: "realsize", Value: "11"})
	test.IsEqualInt(t, w.Code, http.StatusBadRequest)
	test.IsEqualInt(t, len(groupUploadFilesNamed("g7-e2e.txt")), 0)

	// 2. A member: the file is in the group with the group's terms. The
	// client's own settings, here "no expiry", 3 downloads and a password,
	// are ignored, because the group sets them.
	const content = "g7 bytes uploaded into a group"
	w = completeIntoGroup(t, memberKey, "g7member", "g7-member.txt", content, g.Id,
		test.Header{Name: "expiryDays", Value: "0"},
		test.Header{Name: "allowedDownloads", Value: "3"},
		test.Header{Name: "password", Value: "secret"})
	test.IsEqualInt(t, w.Code, http.StatusOK)
	result := struct {
		FileInfo models.FileApiOutput `json:"FileInfo"`
	}{}
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
	fileId := result.FileInfo.Id
	test.IsEqualBool(t, fileId != "", true)
	ids := database.GetGroupFileIds(g.Id)
	test.IsEqualInt(t, len(ids), 1)
	test.IsEqualString(t, ids[0], fileId)
	file, ok := database.GetMetaDataById(fileId)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, file.UnlimitedDownloads, true)
	test.IsEqualBool(t, file.UnlimitedTime, false)
	test.IsEqualInt64(t, file.ExpireAt, g.Expires)
	test.IsEqualString(t, file.PasswordHash, "")
	test.IsEqualInt(t, file.UserId, member.Id)

	// 3. The same bytes again, by another member: the copy already in the
	// group stays, and this one is deleted
	w = completeIntoGroup(t, creatorKey, "g7duplicate", "g7-duplicate.txt", content, g.Id)
	test.IsEqualInt(t, w.Code, http.StatusConflict)
	ids = database.GetGroupFileIds(g.Id)
	test.IsEqualInt(t, len(ids), 1)
	test.IsEqualString(t, ids[0], fileId)
	for _, f := range groupUploadFilesNamed("g7-duplicate.txt") {
		// CleanUp may already have removed it entirely
		test.IsEqualInt64(t, f.ExpireAt, 0)
	}
	// The original is untouched
	file, ok = database.GetMetaDataById(fileId)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt64(t, file.ExpireAt, g.Expires)

	// A group that never closes gives the upload no expiry
	never, err := storage.CreateGroup("Never closes", 0, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(never.Id)
	w = completeIntoGroup(t, creatorKey, "g7never", "g7-never.txt", "never closing content", never.Id,
		test.Header{Name: "expiryDays", Value: "5"})
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
	file, ok = database.GetMetaDataById(result.FileInfo.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, file.UnlimitedTime, true)
	test.IsEqualBool(t, file.UnlimitedDownloads, true)
	test.IsEqualInt(t, len(database.GetGroupFileIds(never.Id)), 1)

	// Without a group id nothing changes: the client's settings apply
	w = completeIntoGroup(t, memberKey, "g7private", "g7-private.txt", "private content", "",
		test.Header{Name: "allowedDownloads", Value: "3"})
	test.IsEqualInt(t, w.Code, http.StatusOK)
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
	file, ok = database.GetMetaDataById(result.FileInfo.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, file.UnlimitedDownloads, false)
	test.IsEqualInt(t, file.DownloadsRemaining, 3)
}
