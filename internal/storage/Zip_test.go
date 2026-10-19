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
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
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
	n := ServeZip([]models.File{ok, limited, expired, request}, "trip", w, r)
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
	test.IsEqualInt(t, ServeZip([]models.File{limited}, "trip", w2, r), 0)
}
