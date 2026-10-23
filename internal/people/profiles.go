package people

// Profiles (Part 4, S1): the display name, the Google name, the switches,
// and what a sign-in captures. The photo itself is in photo.go.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

// ErrDisplayName is the one refusal of a display name
var ErrDisplayName = errors.New("A display name is at most 40 characters, with no control characters.")

// profileLock serialises every read-modify-write of a profile: the photo
// fetch runs in a goroutine and must not undo a Settings save, nor it that
var profileLock sync.Mutex

// profileOf reads the profile without the photo's bytes. Names are looked up
// for every row of every list.
func profileOf(userId int) (models.Profile, bool) {
	if !database.SupportsGroups() {
		return models.Profile{}, false
	}
	return database.GetProfileWithoutPhoto(userId)
}

func photoUrlOf(userId int) string {
	p, ok := profileOf(userId)
	if !ok || p.PhotoType == "" || p.UseInitials {
		return ""
	}
	return fmt.Sprintf("./avatar?id=%d&v=%d", userId, p.PhotoUpdated)
}

func wantsDownloadEmails(userId int) bool {
	p, ok := profileOf(userId)
	return ok && p.EmailDownloads
}

// Of returns the user's profile without the photo's bytes; a person without
// one gets the defaults. For the Settings page.
func Of(userId int) models.Profile {
	p, ok := profileOf(userId)
	if !ok {
		return models.Profile{UserId: userId}
	}
	return p
}

// update applies change to the stored profile (or a new one) under the lock
func update(userId int, change func(p *models.Profile)) {
	if !database.SupportsGroups() {
		return
	}
	profileLock.Lock()
	defer profileLock.Unlock()
	p, ok := database.GetProfile(userId)
	if !ok {
		p = models.Profile{UserId: userId}
	}
	change(&p)
	database.SaveProfile(p)
}

func cleanDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 40 {
		return "", ErrDisplayName
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", ErrDisplayName
		}
	}
	return s, nil
}

// SetDisplayName sets the name shown to others: trimmed, at most 40
// characters, no control characters. Empty goes back to the Google name.
func SetDisplayName(userId int, name string) error {
	clean, err := cleanDisplayName(name)
	if err != nil {
		return err
	}
	update(userId, func(p *models.Profile) { p.DisplayName = clean })
	return nil
}

// SetPreferences changes the switches that are not nil
func SetPreferences(userId int, emailDownloads, useInitials *bool) {
	if emailDownloads == nil && useInitials == nil {
		return
	}
	update(userId, func(p *models.Profile) {
		if emailDownloads != nil {
			p.EmailDownloads = *emailDownloads
		}
		if useInitials != nil {
			p.UseInitials = *useInitials
		}
	})
}

// OnSignIn is the sign-in hook: it keeps Google's name claim, and fetches the
// picture claim when the stored photo is over a day old (in a goroutine,
// unless fetchPhotoSync is set by a test)
func OnSignIn(user models.User, claims map[string]any, _ bool) {
	if !database.SupportsGroups() {
		return
	}
	if raw, ok := claims["name"].(string); ok {
		if name, err := cleanDisplayName(raw); err == nil && name != "" {
			update(user.Id, func(p *models.Profile) { p.GoogleName = name })
		}
	}
	picture, ok := claims["picture"].(string)
	if !ok || picture == "" {
		return
	}
	p, _ := profileOf(user.Id)
	if time.Now().Unix()-p.PhotoUpdated <= 86400 {
		return
	}
	if fetchPhotoSync {
		fetchPhoto(user.Id, picture)
		return
	}
	go fetchPhoto(user.Id, picture)
}
