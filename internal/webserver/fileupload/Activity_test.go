package fileupload

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// A complete-file upload is recorded once, after it is logged (Part 4, A)
func TestProcessRecordsUpload(t *testing.T) {
	database.SaveUser(models.User{Id: 77, Name: "uploader@example.com", UserLevel: models.UserLevelUser}, false)
	defer database.DeleteUser(77)
	w := httptest.NewRecorder()
	err := ProcessCompleteFile(w, getFileUploadRecorder(false), 77, 20)
	test.IsNil(t, err)
	result := models.Result{}
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &result))
	n := 0
	for _, a := range database.GetActivitySince(0, 100000) {
		if a.FileId == result.FileInfo.Id {
			n++
			test.IsEqualString(t, a.Kind, models.ActivityUpload)
			test.IsEqualBool(t, a.ActorId == 77 && a.OwnerId == 77 && a.FileName == "testFile", true)
		}
	}
	test.IsEqualInt(t, n, 1)
}
