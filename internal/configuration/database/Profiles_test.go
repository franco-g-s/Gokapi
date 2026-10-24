package database

import (
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// Part 4, S1: deleting a user deletes their profile. Redis stores none. The
// test opens its own connections: by now an earlier test has closed the
// shared ones.
func TestDeleteUserDeletesProfile(t *testing.T) {
	for _, config := range []models.DbConnection{configRedis, configSqlite} {
		Connect(config)
		SaveUser(models.User{Id: 777, Name: "profile@example.com", UserLevel: models.UserLevelUser}, false)
		SaveProfile(models.Profile{UserId: 777, DisplayName: "Pro", Photo: []byte{1}, PhotoType: "image/png"})
		_, ok := GetProfile(777)
		test.IsEqualBool(t, ok, config.Type == configSqlite.Type)
		_, ok = GetProfileWithoutPhoto(777)
		test.IsEqualBool(t, ok, config.Type == configSqlite.Type)
		DeleteUser(777)
		_, ok = GetProfile(777)
		if ok {
			t.Errorf("database type %d: the profile survived DeleteUser", config.Type)
		}
		Close()
	}
}
