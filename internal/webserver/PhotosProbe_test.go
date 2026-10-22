//go:build !integration && test

package webserver

import (
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestPhotosProbeAccess(t *testing.T) {
	ids := []string{"probephoto", "probebig", "probevideo", "probedoc", "probeother", "probeexpired"}
	defer func() {
		for _, id := range ids {
			database.DeleteMetaData(id)
		}
	}()
	// The admin is user 5
	own := func(name string, size int64) func(*models.File) {
		return func(f *models.File) { f.UserId = 5; f.Name = name; f.SizeBytes = size }
	}
	accessFile("probephoto", own("small.heic", 2000))
	accessFile("probebig", own("big.JPG", 9000))
	accessFile("probevideo", own("clip.mov", 5000))
	accessFile("probedoc", own("notes.pdf", 777))
	accessFile("probeother", func(f *models.File) { f.UserId = 7; f.Name = "theirs.jpg"; f.SizeBytes = 1 })
	accessFile("probeexpired", func(f *models.File) {
		own("old.jpg", 1)(f)
		f.ExpireAt = time.Now().Add(-time.Hour).Unix()
	})
	before, _ := database.GetMetaDataById("probebig")

	// Not an admin: indistinguishable from a page that is not there
	code, _ := filesGet(t, "validsession", "/photosprobe")
	test.IsEqualInt(t, code, 404)
	code, _ = filesGet(t, "", "/photosprobe")
	test.IsEqualInt(t, code, 401)

	code, body := filesGet(t, filesAdminSession().Value, "/photosprobe")
	test.IsEqualInt(t, code, 200)
	page := string(body)
	for _, want := range []string{"probephoto", "probebig", "probevideo", `"kind":"video"`, `"kind":"image"`,
		"./files/download?id=", "photos_probe.min.js"} {
		test.IsEqualBool(t, strings.Contains(page, want), true)
	}
	// Only their own photos and videos: not a PDF, not another person's, not an expired one
	for _, not := range []string{"probedoc", "probeother", "probeexpired", "theirs.jpg", "notes.pdf"} {
		test.IsEqualBool(t, strings.Contains(page, not), false)
	}
	// Largest first
	test.IsEqualBool(t, strings.Index(page, "probebig") < strings.Index(page, "probevideo"), true)
	test.IsEqualBool(t, strings.Index(page, "probevideo") < strings.Index(page, "probephoto"), true)

	// Looking writes nothing
	after, _ := database.GetMetaDataById("probebig")
	test.IsEqualInt(t, after.DownloadCount, before.DownloadCount)
	test.IsEqualInt(t, int(after.SizeBytes), int(before.SizeBytes))
}
