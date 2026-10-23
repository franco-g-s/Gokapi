package webserver

// The Settings page (Part 4, S1): a person's display name, the "Use
// initials" switch and the download emails switch. Every rule lives in
// package people; these handlers only decode, ask and answer.

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/people"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// ViewSettings is the identifier for the Settings page: no tab is active
const ViewSettings = 8

// SettingsView is the Settings page
type SettingsView struct {
	AdminView
	// Name is the shown name, DisplayName what the person typed, and
	// DefaultName what is shown when they type nothing
	Name, DisplayName, DefaultName, Email, Initials string
	HasEmail, EmailDownloads, UseInitials, HasPhoto bool
	// PhotoUrl is the person's own photo, shown here even with "Use initials" on
	PhotoUrl string
	// ReadOnly is set during "View as…": the target's settings, no controls
	ReadOnly  bool
	Supported bool
}

var errSettingsNoEmail = errors.New("Your account has no email address, so it cannot get emails.")
var errSettingsUnsupported = errors.New("Settings need the SQLite database.")

func showSettings(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		redirect(w, r, "login")
		return
	}
	p := people.Of(user.Id)
	defaultName := p.GoogleName
	if defaultName == "" {
		defaultName = people.Name(user)
		if p.DisplayName != "" {
			defaultName = ""
		}
	}
	view := SettingsView{
		AdminView:      *(&AdminView{}).convertGlobalConfig(ViewSettings, user),
		Name:           people.Name(user),
		DisplayName:    p.DisplayName,
		DefaultName:    defaultName,
		Initials:       people.Initials(user),
		HasEmail:       people.HasEmail(user),
		EmailDownloads: p.EmailDownloads,
		UseInitials:    p.UseInitials,
		HasPhoto:       p.PhotoType != "",
		ReadOnly:       viewasActive(r),
		Supported:      database.SupportsGroups(),
	}
	if view.HasEmail {
		view.Email = user.Name
	}
	if view.HasPhoto {
		view.PhotoUrl = fmt.Sprintf("./avatar?id=%d&v=%d", user.Id, p.PhotoUpdated)
	}
	helper.CheckIgnoreTimeout(templateFolder.ExecuteTemplate(w, "settings_page", &view))
}

// settingsRequest is POST /settings/save; a field left out is not changed
type settingsRequest struct {
	DisplayName    *string `json:"displayName"`
	EmailDownloads *bool   `json:"emailDownloads"`
	UseInitials    *bool   `json:"useInitials"`
}

type settingsAnswer struct {
	Result string
	Name   string
}

// saveSettings answers POST /settings/save. A refusal changes nothing.
func saveSettings(w http.ResponseWriter, r *http.Request) {
	var request settingsRequest
	user, ok := groupRequest(w, r, &request)
	if !ok {
		return
	}
	if !database.SupportsGroups() {
		writeJsonError(w, http.StatusNotImplemented, errSettingsUnsupported)
		return
	}
	if request.EmailDownloads != nil && *request.EmailDownloads && !people.HasEmail(user) {
		writeJsonError(w, http.StatusBadRequest, errSettingsNoEmail)
		return
	}
	if request.DisplayName != nil {
		if err := people.SetDisplayName(user.Id, *request.DisplayName); err != nil {
			writeJsonError(w, http.StatusBadRequest, err)
			return
		}
	}
	people.SetPreferences(user.Id, request.EmailDownloads, request.UseInitials)
	writeJson(w, settingsAnswer{Result: "OK", Name: people.Name(user)})
}
