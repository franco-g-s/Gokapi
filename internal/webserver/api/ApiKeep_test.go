//go:build test

package api

import (
	"strconv"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/test"
)

// The Files tab's "Change expiry & limits" leaves out the header of a setting
// set to "Keep as it is". An absent header must leave that setting exactly as
// stored. Characterization: upstream already behaves this way.
func TestEditFileOmittedHeaderKeepsSetting(t *testing.T) {
	ownerKey := generateNewKey(true, idUser, "", "")
	expiry := time.Now().Add(48 * time.Hour).Unix()
	code, _ := modifyFileName(t, ownerKey.Id, idFileUser,
		test.Header{Name: "allowedDownloads", Value: "7"},
		test.Header{Name: "expiryTimestamp", Value: strconv.FormatInt(expiry, 10)})
	test.IsEqualInt(t, code, 200)

	// Only the limit: the expiry stays
	code, _ = modifyFileName(t, ownerKey.Id, idFileUser, test.Header{Name: "allowedDownloads", Value: "3"})
	test.IsEqualInt(t, code, 200)
	file, _ := database.GetMetaDataById(idFileUser)
	test.IsEqualInt(t, file.DownloadsRemaining, 3)
	test.IsEqualBool(t, file.UnlimitedTime, false)
	test.IsEqualInt64(t, file.ExpireAt, expiry)

	// Only the expiry ("never"): the limit stays
	code, _ = modifyFileName(t, ownerKey.Id, idFileUser, test.Header{Name: "expiryTimestamp", Value: "0"})
	test.IsEqualInt(t, code, 200)
	file, _ = database.GetMetaDataById(idFileUser)
	test.IsEqualBool(t, file.UnlimitedTime, true)
	test.IsEqualInt(t, file.DownloadsRemaining, 3)
	test.IsEqualBool(t, file.UnlimitedDownloads, false)
}
