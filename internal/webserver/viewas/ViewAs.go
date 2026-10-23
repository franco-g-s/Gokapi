// Package viewas is "View as…" (Part 3, V1): an admin sees every page with
// another person's visibility, read-only. It is keyed to the admin's user id,
// not a session, so every session of that admin is in it at once.
//
// This package must never import authentication: authentication imports it
// (SessionIdentity swaps to the target), so the session is read through
// sessionmanager instead.
package viewas

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/people"
)

// Idle is how long "View as…" survives without a request
const Idle = time.Hour

// touchEvery limits LastSeen writes to one a minute per admin
const touchEvery = int64(60)

// ErrNotAdmin is returned when someone who is not a real admin starts "View as…"
var ErrNotAdmin = errors.New("only an admin can view the site as someone else")

// ErrBadTarget is returned for yourself, another admin or an unknown user
var ErrBadTarget = errors.New("you can only view the site as an existing person who is not an admin")

// ErrUnsupported is returned on a database without the ViewAs table (Redis)
var ErrUnsupported = errors.New("View as… needs the SQLite database")

// mu serialises every change of state, so an end is logged exactly once and
// Touch can never bring back a row that End just deleted
var mu sync.Mutex

// activeIds is the in-memory set of admins in "View as…", written through on
// Start, End, the sweep and the ticker. The guard's fast path reads only this.
var (
	activeMu  sync.RWMutex
	activeIds = map[int]bool{}
)

func setActive(adminId int, on bool) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if on {
		activeIds[adminId] = true
	} else {
		delete(activeIds, adminId)
	}
}

func isActive(adminId int) bool {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return activeIds[adminId]
}

func anyActive() bool {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return len(activeIds) > 0
}

// IsRealAdmin is true for a logged-in admin; never for the Disabled-mode
// pseudo user #0
func IsRealAdmin(u models.User) bool {
	return u.IsAdmin() && u.Id != 0
}

// Start puts the admin into "View as…" the target. A current "View as…" is
// ended with "switched" first. The admin's temporary API tokens are deleted,
// so a page loaded before the start cannot keep acting as the admin.
func Start(admin models.User, targetId int) error {
	if !IsRealAdmin(admin) {
		return ErrNotAdmin
	}
	if !database.SupportsGroups() {
		return ErrUnsupported
	}
	target, ok := database.GetUser(targetId)
	if !ok || target.Id == admin.Id || target.IsAdmin() {
		return ErrBadTarget
	}
	mu.Lock()
	defer mu.Unlock()
	endLocked(admin.Id, "switched")
	now := time.Now().Unix()
	database.SaveViewAs(models.ViewAs{AdminId: admin.Id, TargetId: target.Id, Started: now, LastSeen: now})
	setActive(admin.Id, true)
	// After the row is saved, so no token can be issued in between
	deleteSystemKeys(admin.Id)
	logging.LogViewAsStart(admin, target)
	return nil
}

func deleteSystemKeys(adminId int) {
	for _, key := range database.GetAllApiKeys() {
		if key.IsSystemKey && key.UserId == adminId {
			database.DeleteApiKey(key.Id)
		}
	}
}

// End ends the admin's "View as…" and logs why. It logs only if one was active.
func End(adminId int, reason string) {
	mu.Lock()
	defer mu.Unlock()
	endLocked(adminId, reason)
}

func endLocked(adminId int, reason string) {
	row, ok := database.GetViewAs(adminId)
	setActive(adminId, false)
	if !ok {
		return
	}
	database.DeleteViewAs(adminId)
	logging.LogViewAsEnd(userOrUnknown(adminId), userOrUnknown(row.TargetId), reason)
}

func userOrUnknown(id int) models.User {
	u, ok := database.GetUser(id)
	if !ok {
		return models.User{Id: id, Name: "(deleted user)"}
	}
	return u
}

// Target returns whom the admin is viewing as: active, not idle past Idle,
// and still an existing non-admin. Read-only: an idle row is left for the
// ticker to end and log.
func Target(adminId int) (models.User, bool) {
	if !isActive(adminId) {
		return models.User{}, false
	}
	row, ok := database.GetViewAs(adminId)
	if !ok || isIdle(row, time.Now().Unix()) {
		return models.User{}, false
	}
	target, ok := database.GetUser(row.TargetId)
	if !ok || target.IsAdmin() {
		return models.User{}, false
	}
	return target, true
}

func isIdle(row models.ViewAs, now int64) bool {
	return now-row.LastSeen > int64(Idle/time.Second)
}

// Touch records a request of the admin: LastSeen = now, written at most once a minute
func Touch(adminId int) {
	row, ok := database.GetViewAs(adminId)
	now := time.Now().Unix()
	if !ok || now-row.LastSeen < touchEvery {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	// Re-read under the lock: End may have deleted the row meanwhile
	row, ok = database.GetViewAs(adminId)
	if !ok {
		return
	}
	row.LastSeen = now
	database.SaveViewAs(row)
}

// SweepAtStartup ends every "View as…" left over from before a restart
func SweepAtStartup() {
	mu.Lock()
	defer mu.Unlock()
	for _, row := range database.GetAllViewAs() {
		endLocked(row.AdminId, "restart")
	}
}

// StartTicker ends, every minute, each "View as…" idle for longer than Idle
func StartTicker() {
	go func() {
		for range time.Tick(time.Minute) {
			endIdle()
		}
	}()
}

// endIdle is one tick of StartTicker
func endIdle() {
	if !anyActive() {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	now := time.Now().Unix()
	for _, row := range database.GetAllViewAs() {
		if admin, ok := database.GetUser(row.AdminId); !ok || !IsRealAdmin(admin) {
			// No longer an admin, or deleted: as in the guard
			endLocked(row.AdminId, "logout")
		} else if isIdle(row, now) {
			endLocked(row.AdminId, "timeout")
		}
	}
}

type realUserKey struct{}

// MarkSwapped notes on the request the real admin behind a swapped user
func MarkSwapped(r *http.Request, real models.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), realUserKey{}, real))
}

// RealUser returns the admin behind a swapped request
func RealUser(r *http.Request) (models.User, bool) {
	u, ok := r.Context().Value(realUserKey{}).(models.User)
	return u, ok
}

// displayName is webserver's displayName (D5), copied: this package cannot
// import webserver. Only the part of the name before "@" is shown.
func displayName(u models.User) string { return people.Name(u) }
