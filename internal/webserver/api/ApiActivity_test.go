package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/forceu/gokapi/internal/activity"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/errorHandling/errorcodes"
)

func activityKeys() (admin, adminNoPerm, user string) {
	database.SaveApiKey(models.ApiKey{Id: "ActivityAdmin", PublicId: "ActivityAdmin", Permissions: models.ApiPermManageLogs, UserId: idAdmin})
	database.SaveApiKey(models.ApiKey{Id: "ActivityAdminNoPerm", PublicId: "ActivityAdminNoPerm", Permissions: models.ApiPermView, UserId: idAdmin})
	database.SaveApiKey(models.ApiKey{Id: "ActivityUser", PublicId: "ActivityUser", Permissions: models.ApiPermManageLogs, UserId: idUser})
	return "ActivityAdmin", "ActivityAdminNoPerm", "ActivityUser"
}

func activityCall(t *testing.T, key string, headers []test.Header) (int, map[string]any) {
	t.Helper()
	w, r := getRecorder("/api/activity", key, headers)
	Process(w, r)
	var parsed map[string]any
	test.IsNil(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	return w.Code, parsed
}

func TestApiActivity(t *testing.T) {
	admin, adminNoPerm, user := activityKeys()
	defer func() {
		for _, k := range []string{admin, adminNoPerm, user} {
			database.DeleteApiKey(k)
		}
	}()
	database.PruneActivity(1 << 62)

	// Without ManageLogs on the key: 401
	code, body := activityCall(t, adminNoPerm, nil)
	test.IsEqualInt(t, code, http.StatusUnauthorized)
	test.IsEqualBool(t, body["ErrorCode"].(float64) == float64(errorcodes.InvalidApiKey), true)
	// A non-admin key, even with ManageLogs: 401 AdminOnly
	code, body = activityCall(t, user, nil)
	test.IsEqualInt(t, code, http.StatusUnauthorized)
	test.IsEqualBool(t, body["ErrorCode"].(float64) == float64(errorcodes.AdminOnly), true)
	// Bad cursors: 400
	code, _ = activityCall(t, admin, []test.Header{{Name: "since", Value: "abc"}})
	test.IsEqualInt(t, code, http.StatusBadRequest)
	code, _ = activityCall(t, admin, []test.Header{{Name: "since", Value: "-1"}})
	test.IsEqualInt(t, code, http.StatusBadRequest)
	code, _ = activityCall(t, admin, []test.Header{{Name: "limit", Value: "x"}})
	test.IsEqualInt(t, code, http.StatusBadRequest)

	activity.Upload(models.File{Id: "act1", Name: "IMG_0001.HEIC", SizeBytes: 2345678, UserId: idUser}, models.User{Id: idUser})
	activity.Download(models.File{Id: "act1", Name: "IMG_0001.HEIC", SizeBytes: 2345678, UserId: idUser}, 0, models.DownloadViaFile)

	// No since header: start from now
	code, body = activityCall(t, admin, nil)
	test.IsEqualInt(t, code, http.StatusOK)
	for _, key := range []string{"Result", "events", "next", "more", "reset", "users"} {
		_, ok := body[key]
		test.IsEqualBool(t, ok, true)
	}
	test.IsEqualString(t, body["Result"].(string), "OK")
	test.IsEqualInt(t, len(body["events"].([]any)), 0)
	newest := body["next"].(float64)
	test.IsEqualBool(t, newest > 0, true)

	// since: 0 is the oldest page
	code, body = activityCall(t, admin, []test.Header{{Name: "since", Value: "0"}})
	test.IsEqualInt(t, code, http.StatusOK)
	events := body["events"].([]any)
	test.IsEqualInt(t, len(events), 2)
	first := events[0].(map[string]any)
	test.IsEqualString(t, first["kind"].(string), "upload")
	file := first["file"].(map[string]any)
	test.IsEqualString(t, file["name"].(string), "IMG_0001.HEIC")
	test.IsEqualString(t, file["kind"].(string), "image")
	test.IsEqualBool(t, file["size"].(float64) == 2345678, true)
	test.IsEqualBool(t, first["group"] == nil, true)
	test.IsEqualBool(t, first["actor"].(float64) == idUser && first["owner"].(float64) == idUser, true)
	test.IsEqualBool(t, body["next"].(float64) == newest && body["more"] == false && body["reset"] == false, true)

	// limit pages
	code, body = activityCall(t, admin, []test.Header{{Name: "since", Value: "0"}, {Name: "limit", Value: "1"}})
	test.IsEqualInt(t, code, http.StatusOK)
	test.IsEqualBool(t, len(body["events"].([]any)) == 1 && body["more"] == true, true)

	// users: every user, with the shown name and the email setting
	users := body["users"].([]any)
	found := false
	for _, raw := range users {
		u := raw.(map[string]any)
		if u["id"].(float64) == idUser {
			found = true
			test.IsEqualString(t, u["email"].(string), "testuser")
			test.IsEqualString(t, u["name"].(string), "testuser")
			test.IsEqualBool(t, u["admin"] == false, true)
			test.IsEqualBool(t, u["emailDownloads"] == false, true)
		}
		if u["id"].(float64) == idAdmin {
			test.IsEqualBool(t, u["admin"] == true, true)
		}
	}
	test.IsEqualBool(t, found, true)
}
