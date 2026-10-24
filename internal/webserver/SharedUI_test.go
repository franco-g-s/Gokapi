//go:build !integration && test

package webserver

// Part 4, S2: display names and Google photos in the shared UI, the user
// menu's Settings entry and the Upload page's download-emails switch.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/people"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/webserver/viewas"
)

// tileBlockOf is a file's whole <figure> on the page, badge included
func tileBlockOf(t *testing.T, page, fileId string) string {
	t.Helper()
	match := regexp.MustCompile(`(?s)<figure class="fgs-tile" data-mine="[^"]*" data-id="` + fileId + `".*?</figure>`).FindString(page)
	if match == "" {
		t.Fatalf("no tile for %s", fileId)
	}
	return match
}

// itemOfUploader is the first feed item uploaded by userId
func itemOfUploader(t *testing.T, answer listAnswer, userId int) listItemJson {
	t.Helper()
	for _, item := range answer.Items {
		if item.UploaderId == userId {
			return item
		}
	}
	t.Fatalf("no item by %d", userId)
	return listItemJson{}
}

func TestUploaderPhotoOnGroupItems(t *testing.T) {
	fx := newGroupPageFixture(t)
	g := fx.group
	// carla (71) has a photo and a display name; anna (72) has neither
	test.IsNil(t, people.StorePhoto(fx.creator.Id, avatarJpeg))
	test.IsNil(t, people.SetDisplayName(fx.creator.Id, "Carla Muster"))
	photo := people.PhotoUrl(fx.creator)
	test.IsEqualBool(t, strings.HasPrefix(photo, "./avatar?id=71&v="), true)

	// The page: carla's tile badge is her photo, with her initials kept for
	// the fallback; anna's is her initials
	w := groupPageCall(showGroup, "GET", "/g?id="+g.Id, fx.member, "")
	test.IsEqualInt(t, w.Code, http.StatusOK)
	page := w.Body.String()
	carlas := tileBlockOf(t, page, fx.b.Id)
	test.IsEqualBool(t, strings.Contains(carlas, `data-uploader="Carla Muster"`), true)
	test.IsEqualBool(t, regexp.MustCompile(`<span class="fgs-initials fgs-has-photo" aria-hidden="true" data-initials="CM"><img class="fgs-avatar-img" src="`+
		regexp.QuoteMeta(strings.ReplaceAll(photo, "&", "&amp;"))+`" alt="" loading="lazy" referrerpolicy="no-referrer"`).MatchString(carlas), true)
	annas := tileBlockOf(t, page, fx.a.Id)
	test.IsEqualBool(t, strings.Contains(annas, `<span class="fgs-initials" aria-hidden="true">A</span>`), true)
	test.IsEqualBool(t, strings.Contains(annas, "fgs-avatar-img"), false)

	// The members: the display name, the photo in the list and the stack
	members := regexp.MustCompile(`(?s)<ul class="fgs-group-people.*?</ul>`).FindString(page)
	test.IsEqualBool(t, strings.Contains(members, `<span class="fgs-group-person-name me-auto">Carla Muster</span>`), true)
	test.IsEqualBool(t, strings.Contains(members, `data-photo="`+strings.ReplaceAll(photo, "&", "&amp;")+`"`), true)
	test.IsEqualBool(t, strings.Contains(members, `<span class="fgs-group-person-name me-auto">anna</span>`), true)
	stack := regexp.MustCompile(`(?s)<span class="fgs-avatar-stack".*?</span>\s*<span class="fgs-members-count"`).FindString(page)
	test.IsEqualInt(t, strings.Count(stack, `class="fgs-avatar-img"`), 1)

	// The feed (/gl) carries the photo URL; anna's items carry ""
	answer := decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+g.Id, &fx.member, ""))
	test.IsEqualString(t, itemOfUploader(t, answer, fx.creator.Id).UploaderPhoto, photo)
	test.IsEqualString(t, itemOfUploader(t, answer, fx.member.Id).UploaderPhoto, "")
	raw := feedCall(groupListFeed, "GET", "/gl?id="+g.Id, &fx.member, "").Body.String()
	test.IsEqualBool(t, strings.Contains(raw, `"UploaderPhoto":""`), true)

	// /groups/item, which the live add reads
	item := groupPageCall(groupItem, "GET", "/groups/item?id="+g.Id+"&file="+fx.b.Id, fx.member, "")
	var got map[string]any
	test.IsNil(t, json.Unmarshal(item.Body.Bytes(), &got))
	test.IsEqualString(t, got["UploaderPhoto"].(string), photo)
	test.IsEqualString(t, got["UploaderName"].(string), "Carla Muster")

	// The Files tab of a member without PERM_LIST (/files/list): carla's
	// files reach anna through the group, with the photo
	files := decodeList(t, feedCall(fileListFeed, "GET", "/files/list?src=files&scope=all", &fx.member, ""))
	test.IsEqualString(t, itemOfUploader(t, files, fx.creator.Id).UploaderPhoto, photo)
	test.IsEqualString(t, itemOfUploader(t, files, fx.creator.Id).UploaderName, "Carla Muster")

	// "Use initials instead": no photo anywhere, the initials stay
	on := true
	people.SetPreferences(fx.creator.Id, nil, &on)
	w = groupPageCall(showGroup, "GET", "/g?id="+g.Id, fx.member, "")
	carlas = tileBlockOf(t, w.Body.String(), fx.b.Id)
	test.IsEqualBool(t, strings.Contains(carlas, `<span class="fgs-initials" aria-hidden="true">CM</span>`), true)
	test.IsEqualBool(t, strings.Contains(w.Body.String(), "fgs-avatar-img"), false)
	answer = decodeList(t, feedCall(groupListFeed, "GET", "/gl?id="+g.Id, &fx.member, ""))
	test.IsEqualString(t, itemOfUploader(t, answer, fx.creator.Id).UploaderPhoto, "")
}

func TestOtherPeoplesNamesAreShownNames(t *testing.T) {
	fx := newGroupPageFixture(t)
	test.IsNil(t, people.SetDisplayName(fx.creator.Id, "Carla Muster"))
	// The admin's Files tab with every upload: "Uploaded by" is the shown name
	files := decodeList(t, feedCall(fileListFeed, "GET", "/files/list?src=files&scope=all", &fx.admin, ""))
	item := itemOfUploader(t, files, fx.creator.Id)
	test.IsEqualString(t, item.UploaderName, "Carla Muster")
	// Only group items carry a photo: an admin's Files tab has no badges
	test.IsEqualString(t, item.UploaderPhoto, "")
}

func TestBrowserCarriesOwnShownNameAndPhoto(t *testing.T) {
	fx := newGroupPageFixture(t)
	test.IsNil(t, people.StorePhoto(fx.member.Id, avatarJpeg))
	test.IsNil(t, people.SetDisplayName(fx.member.Id, "Anna <b>x</b>"))
	page := groupPageCall(showGroup, "GET", "/g?id="+fx.group.Id, fx.member, "").Body.String()
	test.IsEqualBool(t, strings.Contains(page, `data-user-name="Anna &lt;b&gt;x&lt;/b&gt;"`), true)
	test.IsEqualBool(t, strings.Contains(page, `data-user-photo="`+strings.ReplaceAll(people.PhotoUrl(fx.member), "&", "&amp;")+`"`), true)
	test.IsEqualBool(t, strings.Contains(page, "<b>x</b>"), false)
}

// s2Users saves 9500 Anna (an email account) with a session
func s2Users(t *testing.T) models.User {
	t.Helper()
	anna := models.User{Id: 9500, Name: "anna.s2@example.com", UserLevel: models.UserLevelUser}
	database.SaveUser(anna, false)
	database.SaveSession("s2session9500", models.Session{RenewAt: 2147483645, ValidUntil: 2147483646, UserId: anna.Id})
	t.Cleanup(func() {
		database.DeleteSession("s2session9500")
		database.DeleteUser(anna.Id)
	})
	return anna
}

func TestUserMenu(t *testing.T) {
	anna := s2Users(t)
	test.IsNil(t, people.SetDisplayName(anna.Id, "Anna Muster"))
	resp, body := groupsTabDo(t, "GET", "/files", "s2session9500", "", "")
	test.IsEqualInt(t, resp.StatusCode, http.StatusOK)
	menu := regexp.MustCompile(`(?s)<div id="fgs-usermenu".*?</ul>`).FindString(body)
	test.IsEqualBool(t, menu != "", true)
	test.IsEqualBool(t, strings.Contains(menu, "anna.s2@example.com"), true)
	name := strings.Index(menu, `<span class="dropdown-item-text fgs-usermenu-name">Anna Muster`)
	email := strings.Index(menu, `<small class="text-muted">anna.s2@example.com</small>`)
	settings := strings.Index(menu, `<a class="dropdown-item" href="./settings">Settings</a>`)
	divider := strings.Index(menu, `<hr class="dropdown-divider">`)
	logout := strings.Index(menu, `Logout</a>`)
	if name < 0 || email < name || settings < email || divider < settings || logout < divider {
		t.Errorf("user menu order: name %d, email %d, settings %d, divider %d, logout %d\n%s", name, email, settings, divider, logout, menu)
	}
	// The badge carries the shown name, not the account
	badge := regexp.MustCompile(`(?s)<span class="badge[^"]*">.*?</span>`).FindString(menu)
	test.IsEqualBool(t, strings.Contains(badge, "Anna Muster"), true)
	test.IsEqualBool(t, strings.Contains(badge, "anna.s2@"), false)

	// An account without an email address has no second line
	_, body = groupsTabDo(t, "GET", "/files", "validsession", "", "")
	menu = regexp.MustCompile(`(?s)<div id="fgs-usermenu".*?</ul>`).FindString(body)
	test.IsEqualBool(t, strings.Contains(menu, `<small class="text-muted">`), false)
	test.IsEqualBool(t, strings.Contains(menu, `href="./settings"`), true)
}

func TestUploadEmailSwitch(t *testing.T) {
	anna := s2Users(t)
	switchTag := regexp.MustCompile(`<input[^>]*id="fgs-email-downloads"[^>]*>`)
	_, body := groupsTabDo(t, "GET", "/admin", "s2session9500", "", "")
	tag := switchTag.FindString(body)
	test.IsEqualBool(t, tag != "", true)
	test.IsEqualBool(t, strings.Contains(tag, "checked"), false)
	test.IsEqualBool(t, strings.Contains(body, `<label class="form-check-label small text-muted" for="fgs-email-downloads">`), true)
	test.IsEqualBool(t, strings.Contains(body, "Email me when my files are downloaded"), true)
	// It follows the setting, and comes after "Combine each upload into one link"
	on := true
	people.SetPreferences(anna.Id, &on, nil)
	_, body = groupsTabDo(t, "GET", "/admin", "s2session9500", "", "")
	test.IsEqualBool(t, strings.Contains(switchTag.FindString(body), "checked"), true)
	test.IsEqualBool(t, strings.Index(body, "Combine each upload into one link") < strings.Index(body, `id="fgs-email-downloads"`), true)

	// Not for an account without an email address
	_, body = groupsTabDo(t, "GET", "/admin", "validsession", "", "")
	test.IsEqualBool(t, strings.Contains(body, `id="fgs-email-downloads"`), false)

	// Not during "View as…": saving would be refused
	fx := newViewAsFixture(t, 9510)
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	_, body = groupsTabDo(t, "GET", "/admin", fx.adminSession, "", "")
	test.IsEqualBool(t, strings.Contains(body, `id="fgs-email-downloads"`), false)
}

func TestUsersPageShowsShownNameAndEmail(t *testing.T) {
	anna := s2Users(t)
	test.IsNil(t, people.SetDisplayName(anna.Id, "Anna <Muster>"))
	page := getPage(t, "http://localhost:53843/users", filesAdminSession())
	row := userRow(t, page, "9500")
	test.IsEqualBool(t, strings.Contains(row, `<span class="fgs-user-shown">Anna &lt;Muster&gt;</span>`), true)
	test.IsEqualBool(t, strings.Contains(row, `<small class="text-muted d-block fgs-user-email">anna.s2@example.com</small>`), true)
	test.IsEqualBool(t, strings.Index(row, "Anna &lt;Muster&gt;") < strings.Index(row, "anna.s2@example.com"), true)
	// Without a profile the shown name is the email prefix; the account under it
	people.SetPreferences(anna.Id, nil, nil)
	test.IsNil(t, people.SetDisplayName(anna.Id, ""))
	row = userRow(t, getPage(t, "http://localhost:53843/users", filesAdminSession()), "9500")
	test.IsEqualBool(t, strings.Contains(row, `<span class="fgs-user-shown">anna.s2</span>`), true)
	test.IsEqualBool(t, strings.Contains(row, `fgs-user-email">anna.s2@example.com</small>`), true)
}

// Part 4 polish: the pill shows the signed-in person's photo when they have
// one, the icon when they chose initials or have none, and the viewed
// person's photo during "View as…"
func TestUserMenuPhoto(t *testing.T) {
	fx := newViewAsFixture(t, 9520)
	pill := func(session string) string {
		_, body := groupsTabDo(t, "GET", "/files", session, "", "")
		menu := regexp.MustCompile(`(?s)<div id="fgs-usermenu".*?</ul>`).FindString(body)
		test.IsEqualBool(t, menu != "", true)
		button := regexp.MustCompile(`(?s)<button type="button" class="fgs-usermenu-btn".*?</button>`).FindString(menu)
		test.IsEqualBool(t, strings.Contains(button, `aria-label="Account menu"`), true)
		return button
	}
	photoOf := func(u models.User) string { return strings.ReplaceAll(people.PhotoUrl(u), "&", "&amp;") }

	// No photo: the icon, no image
	button := pill(fx.bSession)
	test.IsEqualBool(t, strings.Contains(button, `bi-person-fill`), true)
	test.IsEqualBool(t, strings.Contains(button, `<img`), false)

	// A photo: the image, not the icon
	test.IsNil(t, people.StorePhoto(fx.b.Id, avatarJpeg))
	test.IsEqualBool(t, people.PhotoUrl(fx.b) != "", true)
	button = pill(fx.bSession)
	test.IsEqualBool(t, strings.Contains(button, `<img class="fgs-usermenu-photo" src="`+photoOf(fx.b)+`"`), true)
	test.IsEqualBool(t, strings.Contains(button, `bi-person-fill`), false)

	// Chose initials: back to the icon
	on, off := true, false
	people.SetPreferences(fx.b.Id, nil, &on)
	button = pill(fx.bSession)
	test.IsEqualBool(t, strings.Contains(button, `<img`), false)
	test.IsEqualBool(t, strings.Contains(button, `bi-person-fill`), true)
	people.SetPreferences(fx.b.Id, nil, &off)

	// The admin has no photo; during "View as…" b the pill is b's
	test.IsEqualBool(t, strings.Contains(pill(fx.adminSession), `<img`), false)
	test.IsNil(t, viewas.Start(fx.admin, fx.b.Id))
	button = pill(fx.adminSession)
	test.IsEqualBool(t, strings.Contains(button, `src="`+photoOf(fx.b)+`"`), true)
}
