//go:build test

package api

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/test"
)

// D4: while a file is in a live group, its expiry, download limit and
// password follow the group. Renaming still works.
func TestEditFileInGroupLocked(t *testing.T) {
	const id = "groupLockedFile"
	database.SaveMetaData(models.File{Id: id, Name: "photo.jpg", SHA1: "grouplocksha", UserId: idUser,
		ExpireAt: time.Now().Add(24 * time.Hour).Unix(), DownloadsRemaining: 3})
	defer database.DeleteMetaData(id)
	owner := models.User{Id: idUser, UserLevel: models.UserLevelUser}
	g, err := storage.CreateGroup("Iceland trip", 30, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	res, err := storage.AddFilesToGroup(g, []string{id}, owner)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(res.Added), 1)
	inGroup, _ := database.GetMetaDataById(id)
	ownerKey := generateNewKey(true, idUser, "", "")
	const message = `Expiry and download limit follow the group \"Iceland trip\"`

	// Renaming works
	code, _ := modifyFileName(t, ownerKey.Id, id, test.Header{Name: "filename", Value: "renamed.jpg"})
	test.IsEqualInt(t, code, 200)
	test.IsEqualString(t, nameOf(t, id), "renamed.jpg")
	// So does sending the settings it already has, as the edit dialog does
	code, _ = modifyFileName(t, ownerKey.Id, id,
		test.Header{Name: "allowedDownloads", Value: "0"},
		test.Header{Name: "expiryTimestamp", Value: strconv.FormatInt(inGroup.ExpireAt, 10)},
		test.Header{Name: "originalPassword", Value: "true"},
		test.Header{Name: "filename", Value: "kept.jpg"})
	test.IsEqualInt(t, code, 200)
	test.IsEqualString(t, nameOf(t, id), "kept.jpg")

	refused := [][]test.Header{
		{{Name: "allowedDownloads", Value: "5"}},
		{{Name: "expiryTimestamp", Value: strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}},
		{{Name: "expiryTimestamp", Value: "0"}}, // never: not the group's to give
		{{Name: "password", Value: "secret"}},
		{{Name: "allowedDownloads", Value: "5"}, {Name: "filename", Value: "half.jpg"}}, // nothing is half-applied
	}
	for _, headers := range refused {
		code, body := modifyFileName(t, ownerKey.Id, id, headers...)
		test.IsEqualInt(t, code, 400)
		test.IsEqualBool(t, strings.Contains(body, message), true)
	}
	after, _ := database.GetMetaDataById(id)
	test.IsEqualString(t, after.Name, "kept.jpg")
	test.IsEqualBool(t, after.UnlimitedDownloads, true)
	test.IsEqualInt64(t, after.ExpireAt, inGroup.ExpireAt)
	test.IsEqualString(t, after.PasswordHash, "")

	// Once the group has closed, the file is the uploader's to edit again
	g.Expires = time.Now().Unix() - 1
	database.SaveGroup(g)
	for _, headers := range refused[:4] {
		code, _ := modifyFileName(t, ownerKey.Id, id, headers...)
		test.IsEqualInt(t, code, 200)
	}
	after, _ = database.GetMetaDataById(id)
	test.IsEqualInt(t, after.DownloadsRemaining, 5)
	test.IsEqualBool(t, after.UnlimitedDownloads, false)
	test.IsEqualBool(t, after.UnlimitedTime, true)
	test.IsNotEqualString(t, after.PasswordHash, "")
}
