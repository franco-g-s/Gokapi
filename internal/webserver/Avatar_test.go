//go:build !integration && test

package webserver

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/people"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

var avatarJpeg = append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{1}, 100)...)

func itoa(i int) string { return strconv.Itoa(i) }

func TestAvatar(t *testing.T) {
	settingsUsers(t)
	test.IsNil(t, people.StorePhoto(9400, avatarJpeg))
	g, err := storage.CreateGroup("Avatar trip", 60, models.User{Id: 9400, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)
	t.Cleanup(func() { database.DeleteGroup(g.Id) })
	_, err = storage.JoinGroupByToken(g.InviteToken, models.User{Id: 9402, UserLevel: models.UserLevelUser})
	test.IsNil(t, err)

	url := "/avatar?id=9400&v=1"
	for _, session := range []string{"s1session9400", "s1session5", "s1session9402"} {
		resp, body := groupsTabDo(t, "GET", url, session, "", "")
		test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
		test.IsEqualString(t, resp.Header.Get("Content-Type"), "image/jpeg")
		test.IsEqualString(t, resp.Header.Get("X-Content-Type-Options"), "nosniff")
		test.IsEqualString(t, resp.Header.Get("Cache-Control"), "private, max-age=31536000, immutable")
		test.IsEqualString(t, resp.Header.Get("Pragma"), "")
		test.IsEqualBool(t, body == string(avatarJpeg), true)
	}
	// A stranger: 404, never 403
	resp, _ := groupsTabDo(t, "GET", url, "s1session9401", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusNotFound)
	// No photo, a bad id, no id
	for _, path := range []string{"/avatar?id=9401", "/avatar?id=abc", "/avatar", "/avatar?id=-1"} {
		resp, _ = groupsTabDo(t, "GET", path, "s1session9401", "", "")
		test.IsEqualInt(t, resp.StatusCode, http.StatusNotFound)
	}
	// No session: the login page
	resp, _ = groupsTabDo(t, "GET", url, "", "", "")
	test.IsEqualBool(t, resp.StatusCode >= 300 && resp.StatusCode < 400, true)
	test.IsEqualBool(t, strings.Contains(resp.Header.Get("Location"), "login"), true)

	// "Use initials instead": only the person themself still gets it
	on := true
	people.SetPreferences(9400, nil, &on)
	resp, _ = groupsTabDo(t, "GET", url, "s1session9400", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
	for _, session := range []string{"s1session5", "s1session9402"} {
		resp, _ = groupsTabDo(t, "GET", url, session, "", "")
		test.IsEqualInt(t, resp.StatusCode, http.StatusNotFound)
	}
}

func TestSeedPhotoRoute(t *testing.T) {
	settingsUsers(t)
	body := `{"userId":9401,"jpegBase64":"` + base64.StdEncoding.EncodeToString(avatarJpeg) + `"}`

	// The test server started without GOKAPI_E2E_SEED: the route does not exist
	resp, _ := groupsTabDo(t, "POST", "/seed/photo", "s1session5", "application/json", body)
	test.IsEqualInt(t, resp.StatusCode, http.StatusNotFound)
	_, _, _, ok := people.PhotoOf(9401)
	test.IsEqualBool(t, ok, false)

	send := func(enabled bool, session, body string) int {
		mux := http.NewServeMux()
		registerSeedRoutes(mux, enabled)
		r := httptest.NewRequest("POST", "/seed/photo", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "session_token", Value: session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	test.IsEqualInt(t, send(false, "s1session5", body), http.StatusNotFound)
	test.IsEqualInt(t, send(true, "s1session9400", body), http.StatusForbidden)
	_, _, _, ok = people.PhotoOf(9401)
	test.IsEqualBool(t, ok, false)
	// The same checks as a fetched photo
	test.IsEqualInt(t, send(true, "s1session5", `{"userId":9401,"jpegBase64":"`+
		base64.StdEncoding.EncodeToString([]byte("<html></html>"))+`"}`), http.StatusBadRequest)
	test.IsEqualInt(t, send(true, "s1session5", `{"userId":9401,"jpegBase64":"!!!"}`), http.StatusBadRequest)
	test.IsEqualInt(t, send(true, "s1session5", `{"userId":999999,"jpegBase64":"`+
		base64.StdEncoding.EncodeToString(avatarJpeg)+`"}`), http.StatusNotFound)
	test.IsEqualInt(t, send(true, "s1session5", body), http.StatusOK)
	data, ct, _, ok := people.PhotoOf(9401)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, ct, "image/jpeg")
	test.IsEqualBool(t, bytes.Equal(data, avatarJpeg), true)
}
