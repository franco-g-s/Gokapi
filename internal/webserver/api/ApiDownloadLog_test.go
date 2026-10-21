package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// The API key's owner is recorded, for one file and for a zip
func TestApiDownloadsAreRecorded(t *testing.T) {
	var files []models.File
	for _, id := range []string{"apidllog0000000001", "apidllog0000000002"} {
		f := models.File{Id: id, Name: id + ".txt", SHA1: "sha" + id, UserId: idUser, SizeBytes: 1,
			ExpireAt: time.Now().Add(time.Hour).Unix(), UnlimitedDownloads: true}
		database.SaveMetaData(f)
		t.Cleanup(func() { database.DeleteMetaData(f.Id); database.DeleteDownloadsOfFile(f.Id) })
		test.IsNil(t, os.WriteFile(filepath.Join(configuration.Get().DataDir, f.SHA1), make([]byte, 16), 0600))
		database.DeleteDownloadsOfFile(f.Id)
		files = append(files, f)
	}
	owner := models.User{Id: idUser}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/download/"+files[0].Id, nil)
	apiDownloadSingle(w, &paramFilesDownloadSingle{Id: files[0].Id, WebRequest: r}, owner, models.ApiKey{})
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualBool(t, database.GetDownloadedFileIds(idUser)[files[0].Id], true)
	test.IsEqualBool(t, database.GetDownloadedFileIds(idUser)[files[1].Id], false)

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/files/downloadzip", nil)
	apiDownloadZip(w, &paramFilesDownloadZip{Ids: []string{files[0].Id, files[1].Id}, WebRequest: r}, owner, models.ApiKey{})
	test.IsEqualInt(t, w.Code, 200)
	test.IsEqualBool(t, database.GetDownloadedFileIds(idUser)[files[1].Id], true)
	test.IsEqualBool(t, database.GetDownloadedFileIds(idSuperAdmin)[files[1].Id], false)
}
