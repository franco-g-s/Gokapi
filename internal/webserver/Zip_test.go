//go:build test

package webserver

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// createTestCollection bundles the unprotected, unencrypted test file
// "unlimitedDownload" and returns the collection id.
func createTestCollection(t *testing.T, password string) string {
	t.Helper()
	collection, err := storage.CreateCollection(storage.CollectionParameters{
		Name:     "t",
		FileIds:  []string{"unlimitedDownload"},
		Password: password,
	}, models.User{Id: 5})
	test.IsNil(t, err)
	return collection.Id
}

func TestCollectionZipRequiresPassword(t *testing.T) {
	id := createTestCollection(t, "secret")
	// Password-protected collection without cookie redirects to the password page
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:         "http://localhost:53843/cz?id=" + id,
		RedirectUrl: "c?id=" + id,
	})
	// Following the redirect lands on the password prompt
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:             "http://localhost:53843/cz?id=" + id,
		IsHtml:          true,
		RequiredContent: []string{"Password"},
	})
}

func TestCollectionZipUnknownId(t *testing.T) {
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:        "http://localhost:53843/cz?id=doesnotexist",
		IsHtml:     true,
		ResultCode: 200,
	})
}

func TestCollectionZipServesArchive(t *testing.T) {
	id := createTestCollection(t, "")
	resp, err := http.Get("http://localhost:53843/cz?id=" + id)
	test.IsNil(t, err)
	defer resp.Body.Close()
	test.IsEqualInt(t, resp.StatusCode, 200)
	test.IsEqualString(t, resp.Header.Get("Content-Type"), "application/zip")
	body, err := io.ReadAll(resp.Body)
	test.IsNil(t, err)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	test.IsNil(t, err)
	test.IsEqualInt(t, len(zr.File), 1)
	rc, err := zr.File[0].Open()
	test.IsNil(t, err)
	content, _ := io.ReadAll(rc)
	test.IsEqualString(t, string(content), "def")
}

func TestCollectionPageOffersZipAndHelp(t *testing.T) {
	id := createTestCollection(t, "")
	test.HttpPageResult(t, test.HttpTestConfig{
		Url:    "http://localhost:53843/c?id=" + id,
		IsHtml: true,
		RequiredContent: []string{
			"Download as .zip",
			"./cz?id=" + id,
			"fgs-help-sheet",
			"Download all 1 files",
		},
	})
}
