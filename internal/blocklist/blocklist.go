// Package blocklist keeps the list of emails and domains that may not sign in
// (Part 4, K). Entries are an exact email or "@domain", compared without case.
package blocklist

import (
	"errors"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/activity"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// ErrAdmin is returned when an entry would block an admin
var ErrAdmin = errors.New("You cannot block an admin.")

// ErrUnsupported is returned on a database without the Blocklist table (Redis)
var ErrUnsupported = errors.New("Blocking needs the SQLite database.")

// ErrEntry is returned for something that is neither an email nor an "@domain"
var ErrEntry = errors.New("Enter an email like name@example.com or a domain like @example.com.")

// onBlocked records every refused attempt as a signin_blocked activity row;
// a var so tests can watch it
var onBlocked = activity.SignInBlocked

// normalise lower-cases and checks an entry: an exact email or "@domain"
func normalise(entry string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(entry))
	if strings.ContainsAny(e, " \t\r\n") || strings.Count(e, "@") != 1 {
		return "", ErrEntry
	}
	_, domain, _ := strings.Cut(e, "@")
	if domain == "" || !strings.Contains(domain, ".") {
		return "", ErrEntry
	}
	return e, nil
}

// covers reports whether a normalised entry blocks the email: the same
// address, or the exact domain after the "@"
func covers(entry, email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	_, domain, found := strings.Cut(e, "@")
	if !found || domain == "" {
		return false
	}
	return entry == e || entry == "@"+domain
}

// Matches reports whether email is blocked, exactly or by its whole domain
func Matches(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	_, domain, found := strings.Cut(e, "@")
	if !found || domain == "" {
		return false
	}
	return database.IsBlocked(e) || database.IsBlocked("@"+domain)
}

// Block adds an entry and signs out everybody it covers. It refuses an entry
// that covers an admin.
func Block(entry string, by models.User) error {
	if !database.SupportsGroups() {
		return ErrUnsupported
	}
	e, err := normalise(entry)
	if err != nil {
		return err
	}
	users := database.GetAllUsers()
	for _, u := range users {
		if u.IsAdmin() && covers(e, u.Name) {
			return ErrAdmin
		}
	}
	database.SaveBlock(models.BlockEntry{Entry: e, Added: time.Now().Unix(), AddedBy: by.Id})
	for _, u := range users {
		if covers(e, u.Name) {
			database.DeleteAllSessionsByUser(u.Id)
		}
	}
	return nil
}

// Unblock removes an entry
func Unblock(entry string) {
	database.DeleteBlock(strings.ToLower(strings.TrimSpace(entry)))
}

// List returns every entry, oldest first
func List() []models.BlockEntry {
	return database.GetAllBlocks()
}

// SessionRefused is the session check: a session whose owner is blocked ends,
// even if Block's one-time sign-out raced with a sign-in or a renewal
func SessionRefused(u models.User) bool { return Matches(u.Name) }

// Guard is the sign-in guard: it refuses a blocked email
func Guard(email string) error {
	if Matches(email) {
		onBlocked(email)
		return authentication.ErrSignInRefused
	}
	return nil
}
