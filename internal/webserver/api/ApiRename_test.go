//go:build test

package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/test"
)

func modifyFileName(t *testing.T, apiKey, fileId string, extra ...test.Header) (int, string) {
	t.Helper()
	headers := append([]test.Header{{Name: "id", Value: fileId}}, extra...)
	w, r := getRecorderWithBody("/files/modify", apiKey, "PUT", headers, nil)
	Process(w, r)
	body, err := io.ReadAll(w.Body)
	test.IsNil(t, err)
	return w.Code, string(body)
}

func nameOf(t *testing.T, id string) string {
	t.Helper()
	file, ok := database.GetMetaDataById(id)
	test.IsEqualBool(t, ok, true)
	return file.Name
}

func TestEditFileRename(t *testing.T) {
	ownerKey := generateNewKey(true, idUser, "", "")
	adminKey := generateNewKey(true, idAdmin, "", "")
	original := nameOf(t, idFileUser)

	// No filename header: the name stays as it is
	code, _ := modifyFileName(t, ownerKey.Id, idFileUser, test.Header{Name: "allowedDownloads", Value: "0"})
	test.IsEqualInt(t, code, 200)
	test.IsEqualString(t, nameOf(t, idFileUser), original)

	// The owner can rename
	code, body := modifyFileName(t, ownerKey.Id, idFileUser, test.Header{Name: "filename", Value: "renamed file.txt"})
	test.IsEqualInt(t, code, 200)
	var output models.FileApiOutput
	test.IsNil(t, json.Unmarshal([]byte(body), &output))
	test.IsEqualString(t, output.Name, "renamed file.txt")
	test.IsEqualString(t, nameOf(t, idFileUser), "renamed file.txt")

	// base64 is supported, for names with non-ANSI characters
	encoded := "base64:" + base64.StdEncoding.EncodeToString([]byte("Grüsse.txt"))
	code, _ = modifyFileName(t, ownerKey.Id, idFileUser, test.Header{Name: "filename", Value: encoded})
	test.IsEqualInt(t, code, 200)
	test.IsEqualString(t, nameOf(t, idFileUser), "Grüsse.txt")

	// A user without the edit-others permission cannot rename someone else's file
	adminFileName := nameOf(t, idFileAdmin)
	code, body = modifyFileName(t, ownerKey.Id, idFileAdmin, test.Header{Name: "filename", Value: "stolen.txt"})
	test.IsEqualInt(t, code, 401)
	test.IsEqualBool(t, strings.Contains(body, "No permission to edit file."), true)
	test.IsEqualString(t, nameOf(t, idFileAdmin), adminFileName)

	// A user with that permission can
	code, _ = modifyFileName(t, adminKey.Id, idFileUser, test.Header{Name: "filename", Value: "by-admin.txt"})
	test.IsEqualInt(t, code, 200)
	test.IsEqualString(t, nameOf(t, idFileUser), "by-admin.txt")

	// Path separators are sanitised, never stored
	code, _ = modifyFileName(t, ownerKey.Id, idFileUser, test.Header{Name: "filename", Value: "../../etc/pass\\wd.txt"})
	test.IsEqualInt(t, code, 200)
	stored := nameOf(t, idFileUser)
	test.IsEqualBool(t, strings.ContainsAny(stored, `/\`), false)
	test.IsEqualBool(t, strings.HasPrefix(stored, "."), false)
	test.IsEqualBool(t, strings.HasSuffix(stored, "wd.txt"), true)

	// An empty name, or one that is empty once sanitised, is rejected and nothing changes
	for _, bad := range []string{"", "   ", "...", "\x01\x02"} {
		before := nameOf(t, idFileUser)
		code, _ = modifyFileName(t, ownerKey.Id, idFileUser, test.Header{Name: "filename", Value: bad})
		test.IsEqualInt(t, code, 400)
		test.IsEqualString(t, nameOf(t, idFileUser), before)
	}
}
