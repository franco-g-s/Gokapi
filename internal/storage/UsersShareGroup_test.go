//go:build test

package storage

import (
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestUsersShareGroup(t *testing.T) {
	creator := models.User{Id: 7, UserLevel: models.UserLevelUser}
	member := models.User{Id: 8, UserLevel: models.UserLevelUser}
	g, err := CreateGroup("Shared photos", 60, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	test.IsEqualBool(t, UsersShareGroup(7, 8), false)
	_, err = JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)
	test.IsEqualBool(t, UsersShareGroup(7, 8), true)
	test.IsEqualBool(t, UsersShareGroup(8, 7), true)
	test.IsEqualBool(t, UsersShareGroup(8, 9), false)
	test.IsEqualBool(t, UsersShareGroup(0, 8), false)

	// A closed group no longer counts
	g.Expires = 1
	database.SaveGroup(g)
	test.IsEqualBool(t, UsersShareGroup(7, 8), false)
}
