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

// update applies change to the stored profile (or a new one) under the
// lock, and saves it unless change returns false
func update(userId int, change func(p *models.Profile) bool) {
	if !database.SupportsGroups() {
		return
	}
	profileLock.Lock()
	defer profileLock.Unlock()
	p, ok := database.GetProfile(userId)
	if !ok {
		p = models.Profile{UserId: userId}
	}
	if change(&p) {
		database.SaveProfile(p)
	}
}

// cleanDisplayName trims s and checks it: at most 40 characters, no control
// (Cc) or format (Cf) characters, so no bidi overrides and no zero-width
// characters, and, unless empty, at least one visible character (a letter,
// mark, number, punctuation or symbol). Controller ruling, fix round 1: it
// is stricter than the brief's validator.
func cleanDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 40 {
		return "", ErrDisplayName
	}
	visible := false
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return "", ErrDisplayName
		}
		if unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S) {
			visible = true
		}
	}
	if s != "" && !visible {
		return "", ErrDisplayName
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
	update(userId, func(p *models.Profile) bool { p.DisplayName = clean; return true })
	return nil
}

// SetPreferences changes the switches that are not nil
func SetPreferences(userId int, emailDownloads, useInitials *bool) {
	if emailDownloads == nil && useInitials == nil {
		return
	}
	update(userId, func(p *models.Profile) bool {
		if emailDownloads != nil {
			p.EmailDownloads = *emailDownloads
		}
		if useInitials != nil {
			p.UseInitials = *useInitials
		}
		return true
	})
}

// OnSignIn is the sign-in hook: it keeps Google's name claim (an invalid one
// is dropped), and fetches the picture claim when the last attempt, failed
// or not, is over a day old. The check and the marking happen under one
// lock, so concurrent sign-ins start one fetch. The fetch runs in a
// goroutine, unless fetchPhotoSync is set by a test.
func OnSignIn(user models.User, claims map[string]any, _ bool) {
	if !database.SupportsGroups() {
		return
	}
	googleName := ""
	if raw, ok := claims["name"].(string); ok {
		if name, err := cleanDisplayName(raw); err == nil {
			googleName = name
		}
	}
	picture, _ := claims["picture"].(string)
	fetch := false
	update(user.Id, func(p *models.Profile) bool {
		changed := false
		if googleName != "" && p.GoogleName != googleName {
			p.GoogleName = googleName
			changed = true
		}
		if now := time.Now().Unix(); picture != "" && now-p.PhotoTried > 86400 {
			p.PhotoTried = now
			fetch, changed = true, true
		}
		return changed
	})
	if !fetch {
		return
	}
	if fetchPhotoSync {
		fetchPhoto(user.Id, picture)
		return
	}
	go fetchPhoto(user.Id, picture)
}
