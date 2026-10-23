package redis

import "github.com/forceu/gokapi/internal/models"

// Profiles need the SQLite database, like groups. On Redis nobody has a
// profile: names fall back to the email prefix and nothing is stored.

func (p DatabaseProvider) GetProfile(userId int) (models.Profile, bool) { return models.Profile{}, false }
func (p DatabaseProvider) GetProfileWithoutPhoto(userId int) (models.Profile, bool) {
	return models.Profile{}, false
}
func (p DatabaseProvider) SaveProfile(profile models.Profile) {}
func (p DatabaseProvider) DeleteProfile(userId int)           {}
