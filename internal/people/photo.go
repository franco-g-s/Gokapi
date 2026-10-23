package people

// The Google profile photo (Part 4, S1): fetched from the picture claim at
// sign-in, at most once a day, from googleusercontent.com only, at most
// 512 KB, and only if it sniffs as JPEG, PNG or WebP. Gokapi serves it itself
// at /avatar, so the browser never talks to Google.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

const maxPhotoBytes = 512 << 10

// photoHostOk is the only place photos may come from; a var for the tests
var photoHostOk = func(u *url.URL) bool {
	return u.Scheme == "https" && strings.HasSuffix(u.Hostname(), ".googleusercontent.com")
}

// checkPhotoRedirect lets a redirect through only to an allowed host. It
// looks photoHostOk up when it runs, so a test's replacement applies.
func checkPhotoRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if !photoHostOk(req.URL) {
		return errors.New("redirect to a host that is not allowed")
	}
	return nil
}

// photoClient fetches photos; a var for the tests
var photoClient = &http.Client{Timeout: 5 * time.Second, CheckRedirect: checkPhotoRedirect}

// fetchPhotoSync makes OnSignIn fetch in the foreground, for the tests
var fetchPhotoSync = false

// ErrPhoto refuses a photo that is empty, too large or not JPEG, PNG or WebP
var ErrPhoto = errors.New("a photo must be a JPEG, PNG or WebP image of at most 512 KB")

func fetchPhoto(userId int, raw string) {
	// It runs in its own goroutine: a panic here (a DB error in StorePhoto
	// panics through helper.Check) would otherwise take the server down
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("people: photo fetch for user", userId, "failed:", r)
		}
	}()
	u, err := url.Parse(raw)
	if err != nil || !photoHostOk(u) {
		return
	}
	resp, err := photoClient.Get(u.String())
	if err != nil {
		fmt.Println("people: photo fetch failed for user", userId, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoBytes+1))
	if err != nil {
		return
	}
	_ = StorePhoto(userId, data)
}

// photoType sniffs data; "" unless it is a JPEG, PNG or WebP of at most 512 KB
func photoType(data []byte) string {
	if len(data) == 0 || len(data) > maxPhotoBytes {
		return ""
	}
	ct := http.DetectContentType(data)
	if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" {
		return ""
	}
	return ct
}

// StorePhoto validates and stores the user's photo. The fetch and the e2e
// seed both store through here, so both get the same checks.
func StorePhoto(userId int, data []byte) error {
	ct := photoType(data)
	if ct == "" {
		return ErrPhoto
	}
	if !database.SupportsGroups() {
		return errors.New("photos need the SQLite database")
	}
	now := time.Now().Unix()
	update(userId, func(p *models.Profile) bool {
		if now <= p.PhotoUpdated {
			now = p.PhotoUpdated + 1 // a new photo always gets a new URL
		}
		p.Photo, p.PhotoType, p.PhotoUpdated = data, ct, now
		return true
	})
	return nil
}

// PhotoOf returns the user's stored photo, whether or not they chose initials
func PhotoOf(userId int) (data []byte, contentType string, updated int64, ok bool) {
	if !database.SupportsGroups() {
		return nil, "", 0, false
	}
	p, found := database.GetProfile(userId)
	if !found || !p.HasPhoto() {
		return nil, "", 0, false
	}
	return p.Photo, p.PhotoType, p.PhotoUpdated, true
}
