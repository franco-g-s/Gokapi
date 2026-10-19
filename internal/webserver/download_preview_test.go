//go:build !integration && test

package webserver

import (
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestDownloadPageShowsPreview(t *testing.T) {
	f := accessFile("dlpreview0000000001", func(f *models.File) { f.SHA1 = "dlsha1"; f.Name = "IMG.JPG" })
	database.SavePreview(models.Preview{Sha1: "dlsha1", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Updated: 1})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/d?id=" + f.Id, IsHtml: true,
		RequiredContent: []string{"./p/view?id=" + f.Id, `class="fgs-download-preview"`},
		// The preview replaces the 70px icon slot, it does not sit inside it
		ExcludedContent: []string{`class="icon-container"`},
	})

	// Password prompt: a ready preview exists, but it must not be shown before the password is entered
	pw := accessFile("dlpreviewpw0000000001", func(f *models.File) { f.SHA1 = "dlsha1pw"; f.PasswordHash = "x" })
	database.SavePreview(models.Preview{Sha1: "dlsha1pw", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Updated: 1})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/d?id=" + pw.Id, IsHtml: true,
		RequiredContent: []string{"Password required"},
		ExcludedContent: []string{"./p/"},
	})

	// End-to-end encrypted: the worker never produces a preview, and the page must not link one
	e2e := accessFile("dlpreviewe2e0000001", func(f *models.File) { f.SHA1 = "dlsha1e2e"; f.Encryption.IsEndToEndEncrypted = true })
	database.SavePreview(models.Preview{Sha1: "dlsha1e2e", Status: models.PreviewReady, Kind: models.PreviewKindImage, Width: 3, Height: 4, Updated: 1})
	test.HttpPageResult(t, test.HttpTestConfig{
		Url: "http://localhost:53843/d?id=" + e2e.Id, IsHtml: true,
		ExcludedContent: []string{"./p/"},
	})
}
