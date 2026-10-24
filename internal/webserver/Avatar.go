package webserver

// /avatar (Part 4, S1): a person's Google photo, served by Gokapi. It is
// visible to the person, the admin and anyone sharing a live group with
// them; to anyone else it does not exist (404, never 403). With "Use initials
// instead" on, only the person themself still gets it, for their Settings.

import (
	"net/http"
	"strconv"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/people"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

func serveAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	viewer, err := authentication.GetUserFromRequest(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id < 0 || !mayShowPhoto(viewer, id) {
		http.NotFound(w, r)
		return
	}
	data, contentType, _, ok := people.PhotoOf(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// The URL carries the photo's timestamp, so a new photo is a new URL
	w.Header().Del("Pragma")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

// mayShowPhoto: yourself always; the admin and people sharing a group only
// while the person shows their photo
func mayShowPhoto(viewer models.User, id int) bool {
	if viewer.Id == id {
		return true
	}
	if people.PhotoUrl(models.User{Id: id}) == "" {
		return false
	}
	return viewer.IsAdmin() || storage.UsersShareGroup(viewer.Id, id)
}
