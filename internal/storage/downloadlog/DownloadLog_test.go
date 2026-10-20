//go:build test

package downloadlog

import (
	"net/http/httptest"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestDownloadLog(t *testing.T) {
	r := httptest.NewRequest("GET", "/downloadFile?id=x", nil)
	_, ok := From(r)
	test.IsEqualBool(t, ok, false)

	tagged := With(r, models.DownloadViaZip, 7)
	src, ok := From(tagged)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, src.Via, models.DownloadViaZip)
	test.IsEqualInt(t, src.UserId, 7)

	// Tagging leaves the original request untouched
	_, ok = From(r)
	test.IsEqualBool(t, ok, false)

	anon, ok := From(With(r, models.DownloadViaFile, 0))
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, anon.UserId, 0)
}
