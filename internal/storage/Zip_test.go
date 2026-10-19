//go:build test

package storage

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/cloudconfig"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/filesystem/s3filesystem/aws"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
)

func zipTestFile(t *testing.T, id, content string, mutate func(*models.File)) models.File {
	t.Helper()
	sha := "ziptest" + id
	err := os.WriteFile(configuration.Get().DataDir+"/"+sha, []byte(content), 0600)
	test.IsNil(t, err)
	f := models.File{Id: id, Name: id + ".jpg", SHA1: sha, SizeBytes: int64(len(content)),
		Size: "1 B", UploadDate: 1755181925, ExpireAt: time.Now().Add(time.Hour).Unix(),
		UnlimitedDownloads: true, UnlimitedTime: false}
	if mutate != nil {
		mutate(&f)
	}
	database.SaveMetaData(f)
	return f
}

func TestServeZipFiltersAndCounts(t *testing.T) {
	ok := zipTestFile(t, "zipok00000000000001", "ORIGINAL-BYTES", nil)
	limited := zipTestFile(t, "ziplim0000000000001", "LIMITED", func(f *models.File) {
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 1
	})
	expired := zipTestFile(t, "zipexp0000000000001", "EXPIRED", func(f *models.File) {
		f.ExpireAt = time.Now().Add(-time.Hour).Unix()
	})
	request := zipTestFile(t, "zipreq0000000000001", "REQUEST", func(f *models.File) {
		f.UploadRequestId = "somerequest"
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/cz", nil)
	n := ServeZip([]models.File{ok, limited, expired, request}, "trip", w, r, true)
	test.IsEqualInt(t, n, 2)

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	test.IsNil(t, err)
	test.IsEqualInt(t, len(zr.File), 2)
	rc, _ := zr.File[0].Open()
	got, _ := io.ReadAll(rc)
	test.IsEqualString(t, string(got), "ORIGINAL-BYTES")
	test.IsEqualInt(t, int(zr.File[0].Method), int(zip.Store))
	test.IsEqualInt(t, database.GetDownloadsRemaining(limited.Id), 0)

	// A second zip no longer includes the exhausted file
	w2 := httptest.NewRecorder()
	test.IsEqualInt(t, ServeZip([]models.File{limited}, "trip", w2, r, true), 0)
}

// The owner's zip from the Files tab spends nothing, but still skips what
// the counted path skips.
func TestServeZipUncounted(t *testing.T) {
	limited := zipTestFile(t, "zipnocount000000001", "LIMITED", func(f *models.File) {
		f.UnlimitedDownloads = false
		f.DownloadsRemaining = 1
	})
	expired := zipTestFile(t, "zipnocountexp000001", "EXPIRED", func(f *models.File) {
		f.ExpireAt = time.Now().Add(-time.Hour).Unix()
	})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		n := ServeZip([]models.File{limited, expired}, "own", w, httptest.NewRequest("GET", "/files/zip", nil), false)
		test.IsEqualInt(t, n, 1)
		test.IsEqualInt(t, w.Code, 200)
	}
	test.IsEqualInt(t, database.GetDownloadsRemaining(limited.Id), 1)
	stored, _ := database.GetMetaDataById(limited.Id)
	test.IsEqualInt(t, stored.DownloadCount, 0)
}

// A remote file with server-side encryption can only be decrypted by the
// client. Zipping it would stream ciphertext that looks like the file, and
// count a download for it.
func TestServeZipSkipsRemoteEncrypted(t *testing.T) {
	if !aws.IsIncludedInBuild {
		t.Fatal("needs the awsmock build tag")
	}
	testconfiguration.EnableS3()
	defer testconfiguration.DisableS3()
	config, ok := cloudconfig.Load()
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, aws.Init(config.Aws), true)

	f := models.File{Id: "zipaws0000000000001", Name: "remote.jpg", Size: "20 MB",
		SHA1: "x341354656543213246465465465432456898794", AwsBucket: "gokapi-test",
		ExpireAt: time.Now().Add(time.Hour).Unix(), DownloadsRemaining: 3, UserId: 5}
	f.Encryption.IsEncrypted = true
	database.SaveMetaData(f)
	// Leave no remote file behind for CleanUp in later tests
	defer database.DeleteMetaData(f.Id)
	_, found := GetFile(f.Id)
	test.IsEqualBool(t, found, true)

	w := httptest.NewRecorder()
	n := ServeZip([]models.File{f}, "trip", w, httptest.NewRequest("GET", "/cz", nil), true)
	test.IsEqualInt(t, n, 0)
	test.IsEqualInt(t, w.Code, 410)
	test.IsEqualInt(t, database.GetDownloadsRemaining(f.Id), 3)
	stored, _ := database.GetMetaDataById(f.Id)
	test.IsEqualInt(t, stored.DownloadCount, 0)
}
