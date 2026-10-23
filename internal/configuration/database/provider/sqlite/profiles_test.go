//go:build test

package sqlite

import (
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestProfilesRoundTrip(t *testing.T) {
	// Own instance: file-name ordering runs this after TestRawSql, which closes dbInstance
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()

	_, ok := instance.GetProfile(501)
	test.IsEqualBool(t, ok, false)
	_, ok = instance.GetProfileWithoutPhoto(501)
	test.IsEqualBool(t, ok, false)

	p := models.Profile{UserId: 501, DisplayName: "Annie", GoogleName: "Anna Muster",
		Photo: []byte{0xff, 0xd8, 0xff, 1, 2}, PhotoType: "image/jpeg", PhotoUpdated: 1792345678,
		UseInitials: true, EmailDownloads: true}
	instance.SaveProfile(p)
	got, ok := instance.GetProfile(501)
	test.IsEqualBool(t, ok, true)
	test.IsEqual(t, got, p)

	light, ok := instance.GetProfileWithoutPhoto(501)
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, light.Photo == nil, true)
	test.IsEqualString(t, light.PhotoType, "image/jpeg")
	test.IsEqualInt64(t, light.PhotoUpdated, 1792345678)
	test.IsEqualString(t, light.DisplayName, "Annie")
	test.IsEqualBool(t, light.UseInitials && light.EmailDownloads, true)

	// No photo: the light read reports none, even if a type was left behind
	p.Photo, p.UseInitials, p.EmailDownloads = nil, false, false
	instance.SaveProfile(p)
	got, _ = instance.GetProfile(501)
	test.IsEqualBool(t, got.HasPhoto(), false)
	test.IsEqualBool(t, got.UseInitials || got.EmailDownloads, false)
	light, _ = instance.GetProfileWithoutPhoto(501)
	test.IsEqualString(t, light.PhotoType, "")

	instance.DeleteProfile(501)
	_, ok = instance.GetProfile(501)
	test.IsEqualBool(t, ok, false)
}
