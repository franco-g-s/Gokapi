// Package people is the one place a person's shown name, initials and photo
// come from (Part 4). Everything a person sees about another person goes
// through it.
package people

import (
	"strings"
	"unicode"

	"github.com/forceu/gokapi/internal/models"
)

// Name is the name shown for u: their display name, else their Google name,
// else the part of the account name before "@" (Part 2's displayName).
func Name(u models.User) string {
	p, ok := profileOf(u.Id)
	if ok && p.DisplayName != "" {
		return p.DisplayName
	}
	return defaultName(u, p)
}

// DefaultName is what an empty display name falls back to: the Google name,
// else the part of the account name before "@"
func DefaultName(u models.User) string {
	p, _ := profileOf(u.Id)
	return defaultName(u, p)
}

func defaultName(u models.User, p models.Profile) string {
	if p.GoogleName != "" {
		return p.GoogleName
	}
	return emailPrefix(u.Name)
}

func emailPrefix(name string) string {
	if before, _, found := strings.Cut(name, "@"); found && before != "" {
		return before
	}
	return name
}

// Initials is the avatar badge for u
func Initials(u models.User) string { return InitialsOfName(Name(u)) }

// InitialsOfName: first letters of the first and last parts, upper case
// ("anna.mueller" -> "AM", "ben" -> "B"). media.js initialsOf mirrors it.
func InitialsOfName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(parts) == 0 {
		return ""
	}
	first := []rune(parts[0])[:1]
	if len(parts) == 1 {
		return strings.ToUpper(string(first))
	}
	last := []rune(parts[len(parts)-1])[:1]
	return strings.ToUpper(string(first) + string(last))
}

// HasEmail reports whether the account name is an email address
func HasEmail(u models.User) bool { return strings.Contains(u.Name, "@") }

// PhotoUrl is the relative URL of u's photo, or "" for initials
func PhotoUrl(u models.User) string { return photoUrlOf(u.Id) }

// WantsDownloadEmails is the "Email me when my files are downloaded" setting
func WantsDownloadEmails(userId int) bool { return wantsDownloadEmails(userId) }
