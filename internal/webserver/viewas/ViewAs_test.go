//go:build test

package viewas

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
)

var logDir string

func TestMain(m *testing.M) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	var err error
	logDir, err = os.MkdirTemp("", "viewaslog")
	if err != nil {
		panic(err)
	}
	logging.Init(logDir)
	exitVal := m.Run()
	_ = os.RemoveAll(logDir)
	testconfiguration.Delete()
	os.Exit(exitVal)
}

// people saves a fresh admin and two fresh non-admins with ids from base,
// and ends any "View as…" of the admin when the test is over
func people(t *testing.T, base int, prefix string) (admin, a, b models.User) {
	t.Helper()
	admin = models.User{Id: base, Name: prefix + "-admin@example.com", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll}
	a = models.User{Id: base + 1, Name: prefix + "-a@example.com", UserLevel: models.UserLevelUser}
	b = models.User{Id: base + 2, Name: prefix + "-b@example.com", UserLevel: models.UserLevelUser}
	for _, u := range []models.User{admin, a, b} {
		database.SaveUser(u, false)
	}
	t.Cleanup(func() {
		End(admin.Id, "exit")
		for _, u := range []models.User{admin, a, b} {
			database.DeleteUser(u.Id)
		}
	})
	return admin, a, b
}

// logLines returns the log lines that mention this admin's name
func logLines(t *testing.T, admin models.User) []string {
	t.Helper()
	content, _ := logging.GetAll()
	var result []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, admin.Name+" (user #") {
			result = append(result, line)
		}
	}
	return result
}

func endLine(admin, target models.User, reason string) string {
	return "[auth] " + admin.Name + " (user #" + itoa(admin.Id) + ") stopped viewing as " +
		target.Name + " (user #" + itoa(target.Id) + "): " + reason
}

func startLine(admin, target models.User) string {
	return "[auth] " + admin.Name + " (user #" + itoa(admin.Id) + ") started viewing as " +
		target.Name + " (user #" + itoa(target.Id) + ")"
}

func hasSuffixLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.HasSuffix(l, want) {
			return true
		}
	}
	return false
}

func TestIsRealAdmin(t *testing.T) {
	test.IsEqualBool(t, IsRealAdmin(models.User{Id: 3, UserLevel: models.UserLevelAdmin}), true)
	test.IsEqualBool(t, IsRealAdmin(models.User{Id: 3, UserLevel: models.UserLevelSuperAdmin}), true)
	test.IsEqualBool(t, IsRealAdmin(models.User{Id: 0, UserLevel: models.UserLevelSuperAdmin}), false)
	test.IsEqualBool(t, IsRealAdmin(models.User{Id: 3, UserLevel: models.UserLevelUser}), false)
}

func TestStartRules(t *testing.T) {
	admin, a, _ := people(t, 8100, "rules")
	otherAdmin := models.User{Id: 8110, Name: "rules-admin2", UserLevel: models.UserLevelAdmin}
	database.SaveUser(otherAdmin, false)
	defer database.DeleteUser(otherAdmin.Id)

	test.IsEqualBool(t, errors.Is(Start(a, admin.Id), ErrNotAdmin), true)
	test.IsEqualBool(t, errors.Is(Start(admin, admin.Id), ErrBadTarget), true)
	test.IsEqualBool(t, errors.Is(Start(admin, otherAdmin.Id), ErrBadTarget), true)
	test.IsEqualBool(t, errors.Is(Start(admin, 8199), ErrBadTarget), true)
	_, ok := database.GetViewAs(admin.Id)
	test.IsEqualBool(t, ok, false)
	test.IsEqualInt(t, len(logLines(t, admin)), 0)

	test.IsNil(t, Start(admin, a.Id))
	target, ok := Target(admin.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, target.Id, a.Id)
	test.IsEqualBool(t, hasSuffixLine(logLines(t, admin), startLine(admin, a)), true)
}

func TestStartDeletesSystemKeys(t *testing.T) {
	admin, a, _ := people(t, 8120, "keys")
	expiry := time.Now().Add(5 * time.Minute).Unix()
	database.SaveApiKey(models.ApiKey{Id: "viewassystemkey000001", PublicId: "viewassystempub000001", IsSystemKey: true, UserId: admin.Id, Expiry: expiry})
	database.SaveApiKey(models.ApiKey{Id: "viewasrealkey00000001", PublicId: "viewasrealpub00000001", IsSystemKey: false, UserId: admin.Id})
	database.SaveApiKey(models.ApiKey{Id: "viewasothersystem0001", PublicId: "viewasothersyspub0001", IsSystemKey: true, UserId: a.Id, Expiry: expiry})
	defer database.DeleteApiKey("viewasrealkey00000001")
	defer database.DeleteApiKey("viewasothersystem0001")

	test.IsNil(t, Start(admin, a.Id))
	_, ok := database.GetApiKey("viewassystemkey000001")
	test.IsEqualBool(t, ok, false)
	_, ok = database.GetApiKey("viewasrealkey00000001")
	test.IsEqualBool(t, ok, true)
	_, ok = database.GetApiKey("viewasothersystem0001")
	test.IsEqualBool(t, ok, true)
}

func TestTargetIdle(t *testing.T) {
	admin, a, _ := people(t, 8130, "idle")
	test.IsNil(t, Start(admin, a.Id))
	row, ok := database.GetViewAs(admin.Id)
	test.IsEqualBool(t, ok, true)
	row.LastSeen = time.Now().Add(-61 * time.Minute).Unix()
	database.SaveViewAs(row)
	_, ok = Target(admin.Id)
	test.IsEqualBool(t, ok, false)
	// Target is read-only: the row is still there for the ticker to end
	_, ok = database.GetViewAs(admin.Id)
	test.IsEqualBool(t, ok, true)
}

func TestTouch(t *testing.T) {
	admin, a, _ := people(t, 8135, "touch")
	test.IsNil(t, Start(admin, a.Id))
	row, _ := database.GetViewAs(admin.Id)
	row.LastSeen = time.Now().Add(-30 * time.Second).Unix()
	database.SaveViewAs(row)
	Touch(admin.Id)
	after, _ := database.GetViewAs(admin.Id)
	test.IsEqualInt64(t, after.LastSeen, row.LastSeen) // written at most once a minute

	row.LastSeen = time.Now().Add(-10 * time.Minute).Unix()
	database.SaveViewAs(row)
	Touch(admin.Id)
	after, _ = database.GetViewAs(admin.Id)
	test.IsEqualBool(t, after.LastSeen >= time.Now().Unix()-1, true)
}

func TestTickerEndsIdle(t *testing.T) {
	admin, a, _ := people(t, 8140, "ticker")
	test.IsNil(t, Start(admin, a.Id))
	row, _ := database.GetViewAs(admin.Id)
	row.LastSeen = time.Now().Add(-61 * time.Minute).Unix()
	database.SaveViewAs(row)

	endIdle()
	_, ok := database.GetViewAs(admin.Id)
	test.IsEqualBool(t, ok, false)
	test.IsEqualBool(t, isActive(admin.Id), false)
	test.IsEqualBool(t, hasSuffixLine(logLines(t, admin), endLine(admin, a, "timeout")), true)
}

func TestTickerKeepsFresh(t *testing.T) {
	admin, a, _ := people(t, 8145, "fresh")
	test.IsNil(t, Start(admin, a.Id))
	endIdle()
	_, ok := database.GetViewAs(admin.Id)
	test.IsEqualBool(t, ok, true)
}

func TestSweepAtStartup(t *testing.T) {
	admin1, a, _ := people(t, 8150, "sweep1")
	admin2, _, b := people(t, 8160, "sweep2")
	test.IsNil(t, Start(admin1, a.Id))
	test.IsNil(t, Start(admin2, b.Id))

	SweepAtStartup()
	test.IsEqualInt(t, len(database.GetAllViewAs()), 0)
	test.IsEqualBool(t, isActive(admin1.Id), false)
	test.IsEqualBool(t, isActive(admin2.Id), false)
	test.IsEqualBool(t, hasSuffixLine(logLines(t, admin1), endLine(admin1, a, "restart")), true)
	test.IsEqualBool(t, hasSuffixLine(logLines(t, admin2), endLine(admin2, b, "restart")), true)
}

func TestSwitchLogsEnd(t *testing.T) {
	admin, a, b := people(t, 8170, "switch")
	test.IsNil(t, Start(admin, a.Id))
	test.IsNil(t, Start(admin, b.Id))
	lines := logLines(t, admin)
	test.IsEqualInt(t, len(lines), 3)
	test.IsEqualBool(t, strings.HasSuffix(lines[0], startLine(admin, a)), true)
	test.IsEqualBool(t, strings.HasSuffix(lines[1], endLine(admin, a, "switched")), true)
	test.IsEqualBool(t, strings.HasSuffix(lines[2], startLine(admin, b)), true)
	target, ok := Target(admin.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, target.Id, b.Id)
}

func TestEndLogsOnce(t *testing.T) {
	admin, a, _ := people(t, 8180, "once")
	End(admin.Id, "exit") // nothing active: silent
	test.IsEqualInt(t, len(logLines(t, admin)), 0)
	test.IsNil(t, Start(admin, a.Id))
	End(admin.Id, "exit")
	End(admin.Id, "exit")
	lines := logLines(t, admin)
	test.IsEqualInt(t, len(lines), 2)
	test.IsEqualBool(t, strings.HasSuffix(lines[1], endLine(admin, a, "exit")), true)
	_, ok := Target(admin.Id)
	test.IsEqualBool(t, ok, false)
}

func itoa(i int) string { return strconv.Itoa(i) }
