package webserver

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// Part 3, P: a temporary, admin-only page on which Franco measures what iOS
// Safari's share sheet accepts, so Save to Photos can batch to fit. It is
// removed before the PR.

// probeItem is one photo or video on the probe page
type probeItem struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Kind  string `json:"kind"`
	Url   string `json:"url"`
}

type photosProbeView struct {
	Items []probeItem
}

var probeImageExts = map[string]bool{"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "heic": true, "heif": true}
var probeVideoExts = map[string]bool{"mov": true, "mp4": true, "m4v": true}

// probeKind is "image", "video" or "" for anything else
func probeKind(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if probeImageExts[ext] {
		return "image"
	}
	if probeVideoExts[ext] {
		return "video"
	}
	return ""
}

// Handling of /photosprobe: read-only, real admins only, 404 for anyone else
func photosProbe(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil || !user.IsAdmin() || user.Id == 0 {
		http.NotFound(w, r)
		return
	}
	config := configuration.Get()
	now := time.Now().Unix()
	items := make([]probeItem, 0)
	for _, f := range database.GetAllMetadata() {
		if f.UserId != user.Id {
			continue
		}
		kind := probeKind(f.Name)
		if kind == "" {
			continue
		}
		out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
		helper.Check(err)
		if !isListedFile(out, now) {
			continue
		}
		url := directDownloadUrl(out)
		if url == "" {
			continue
		}
		items = append(items, probeItem{Id: f.Id, Name: f.Name, Bytes: f.SizeBytes, Kind: kind, Url: url})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Bytes > items[j].Bytes })
	err = templateFolder.ExecuteTemplate(w, "photos_probe", photosProbeView{Items: items})
	helper.CheckIgnoreTimeout(err)
}
