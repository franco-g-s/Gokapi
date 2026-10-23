package dbabstraction

import (
	"fmt"

	"github.com/forceu/gokapi/internal/configuration/database/provider/redis"
	"github.com/forceu/gokapi/internal/configuration/database/provider/sqlite"
	"github.com/forceu/gokapi/internal/models"
)

const (
	// TypeSqlite specifies to use an SQLite database
	TypeSqlite = iota
	// TypeRedis specifies to use a Redis database
	TypeRedis
)

// Database declares the required functions for a database connection
type Database interface {
	// GetType returns identifier of the underlying interface
	GetType() int

	// Upgrade migrates the DB to a new Gokapi version, if required
	Upgrade(currentDbVersion int)
	// RunGarbageCollection runs the databases GC
	RunGarbageCollection()
	// Close the database connection
	Close()

	// GetDbVersion gets the version number of the database
	GetDbVersion() int
	// SetDbVersion sets the version number of the database
	SetDbVersion(newVersion int)
	// GetSchemaVersion returns the version number, that the database should be if fully upgraded
	GetSchemaVersion() int

	// GetAllApiKeys returns a map with all API keys
	GetAllApiKeys() map[string]models.ApiKey
	// GetApiKey returns a models.ApiKey if valid or false if the ID is not valid
	GetApiKey(id string) (models.ApiKey, bool)
	// SaveApiKey saves the API key to the database
	SaveApiKey(apikey models.ApiKey)
	// UpdateTimeApiKey writes the content of LastUsage to the database
	UpdateTimeApiKey(apikey models.ApiKey)
	// DeleteApiKey deletes an API key with the given ID
	DeleteApiKey(id string)
	// GetApiKeyByPublicKey returns an API key by using the public key
	GetApiKeyByPublicKey(publicKey string) (string, bool)

	// SaveEnd2EndInfo stores the encrypted e2e info
	SaveEnd2EndInfo(info models.E2EInfoEncrypted, userId int)
	// GetEnd2EndInfo retrieves the encrypted e2e info
	GetEnd2EndInfo(userId int) models.E2EInfoEncrypted
	// DeleteEnd2EndInfo resets the encrypted e2e info
	DeleteEnd2EndInfo(userId int)

	// GetHotlink returns the id of the file associated or false if not found
	GetHotlink(id string) (string, bool)
	// GetAllHotlinks returns an array with all hotlink ids
	GetAllHotlinks() []string
	// SaveHotlink stores the hotlink associated with the file in the database
	SaveHotlink(file models.File)
	// DeleteHotlink deletes a hotlink with the given hotlink ID
	DeleteHotlink(id string)

	// GetAllMetadata returns a map of all available files
	GetAllMetadata() map[string]models.File
	// GetMetaDataById returns a models.File from the ID passed or false if the id is not valid
	GetMetaDataById(id string) (models.File, bool)
	// SaveMetaData stores the metadata of a file to the disk
	SaveMetaData(file models.File)
	// DeleteMetaData deletes information about a file
	DeleteMetaData(id string)
	// IncreaseDownloadCount increases the download count of a file, preventing race conditions
	IncreaseDownloadCount(id string, decreaseRemainingDownloads bool)
	// GetDownloadsRemaining returns the remaining downloads of a file that does not implement UnlimitedDownloads
	GetDownloadsRemaining(id string) int

	// GetSession returns the session with the given ID or false if not a valid ID
	GetSession(id string) (models.Session, bool)
	// SaveSession stores the given session. After the expiry passed, it will be deleted automatically
	SaveSession(id string, session models.Session)
	// DeleteSession deletes a session with the given ID
	DeleteSession(id string)
	// DeleteAllSessions logs all users out
	DeleteAllSessions()
	// DeleteAllSessionsByUser logs the specific users out
	DeleteAllSessionsByUser(userId int)

	// GetAllUsers returns a map with all users
	GetAllUsers() []models.User
	// GetUser returns a models.User if valid or false if the ID is not valid
	GetUser(id int) (models.User, bool)
	// GetUserByName returns a models.User if valid or false if the username is not valid
	GetUserByName(email string) (models.User, bool)
	// SaveUser saves a user to the database. If isNewUser is true, a new Id will be generated
	SaveUser(user models.User, isNewUser bool)
	// UpdateUserLastOnline writes the last online time to the database
	UpdateUserLastOnline(id int)
	// DeleteUser deletes a user with the given ID
	DeleteUser(id int)

	// GetFileRequest returns the FileRequest or false if not found
	GetFileRequest(id string) (models.FileRequest, bool)
	// GetAllFileRequests returns an array with all file requests, ordered by creation date
	GetAllFileRequests() []models.FileRequest
	// SaveFileRequest stores the file request associated with the file in the database
	SaveFileRequest(request models.FileRequest)
	// DeleteFileRequest deletes a file request with the given ID
	DeleteFileRequest(request models.FileRequest)

	// GetCollection returns the Collection or false if not found
	GetCollection(id string) (models.Collection, bool)
	// GetAllCollections returns all collections, newest first
	GetAllCollections() []models.Collection
	// SaveCollection stores the collection and its member list
	SaveCollection(collection models.Collection)
	// DeleteCollection deletes the collection, leaving the files untouched
	DeleteCollection(id string)
	// RemoveFileFromCollections drops a file from every collection it belongs to
	RemoveFileFromCollections(fileId string)

	// GetPreview returns the preview metadata for a blob hash, or false if not found
	GetPreview(sha1 string) (models.Preview, bool)
	// SavePreview inserts or replaces preview metadata
	SavePreview(p models.Preview)
	// DeletePreview removes preview metadata
	DeletePreview(sha1 string)
	// GetAllPreviewSha1s maps every known blob hash to its preview status
	GetAllPreviewSha1s() map[string]string

	// SupportsGroups is false on providers without group tables (Redis)
	SupportsGroups() bool
	// GetGroup returns the group or false if not found
	GetGroup(id string) (models.Group, bool)
	// GetGroupByToken returns the group with this invite token
	GetGroupByToken(token string) (models.Group, bool)
	// GetGroupByCode returns the group with this join code (as normalised by storage)
	GetGroupByCode(code string) (models.Group, bool)
	// GetAllGroups returns all groups, newest first
	GetAllGroups() []models.Group
	// SaveGroup inserts or replaces a group
	SaveGroup(g models.Group)
	// DeleteGroup removes the group, its members, its file links and its kept
	// near-copies. Files are never touched.
	DeleteGroup(id string)
	// GetGroupMembers returns the members of a group, by Joined ascending
	GetGroupMembers(groupId string) []models.GroupMember
	// GetMembershipsOfUser returns every group membership of a user
	GetMembershipsOfUser(userId int) []models.GroupMember
	// SaveGroupMember inserts or replaces a membership
	SaveGroupMember(m models.GroupMember)
	// DeleteGroupMember removes one membership
	DeleteGroupMember(groupId string, userId int)
	// GetGroupFileIds returns the ids of all files in a group
	GetGroupFileIds(groupId string) []string
	// GetGroupIdsOfFile returns the ids of all groups a file is in
	GetGroupIdsOfFile(fileId string) []string
	// AddGroupFile puts a file into a group (idempotent)
	AddGroupFile(groupId, fileId string)
	// RemoveGroupFile takes a file out of one group
	RemoveGroupFile(groupId, fileId string)
	// RemoveFileFromGroups drops a file from every group
	RemoveFileFromGroups(fileId string)
	// SaveNearCopyKept remembers "Keep both" for a file in a group (idempotent)
	SaveNearCopyKept(groupId, fileId string)
	// GetNearCopiesKept returns the file ids kept in a group despite looking like another's
	GetNearCopiesKept(groupId string) map[string]bool
	// SaveDownload records one original file served to someone
	SaveDownload(d models.Download)
	// GetDownloadedFileIds is every file id the user has downloaded, by any route
	GetDownloadedFileIds(userId int) map[string]bool
	// DeleteDownloadsOfFile removes the download rows of a file
	DeleteDownloadsOfFile(fileId string)
	// CopyDownloads gives every signed-in user who downloaded fromFileId a
	// download row for toFileId too, unless they already have one
	CopyDownloads(fromFileId, toFileId string)
	// SaveCollectionSeen records the first time a user opened a collection; keeps the first FirstOpened
	SaveCollectionSeen(s models.CollectionSeen)
	// GetCollectionsSeen returns the collections a user has opened, newest first
	GetCollectionsSeen(userId int) []models.CollectionSeen
	// GetCollectionViewers returns who opened a collection while signed in, first opened first
	GetCollectionViewers(collectionId string) []models.CollectionSeen
	// SaveCollectionGroup links a collection to the group it was turned into
	SaveCollectionGroup(cg models.CollectionGroup)
	// GetCollectionGroup returns the group link of a collection, or false
	GetCollectionGroup(collectionId string) (models.CollectionGroup, bool)
	// GetCollectionGroupsOfGroup returns the collections linked to a group
	GetCollectionGroupsOfGroup(groupId string) []models.CollectionGroup
	// DeleteCollectionGroup removes the group link of a collection
	DeleteCollectionGroup(collectionId string)

	// GetViewAs returns the admin's active "View as…", or false
	GetViewAs(adminId int) (models.ViewAs, bool)
	// GetAllViewAs returns every active "View as…"
	GetAllViewAs() []models.ViewAs
	// SaveViewAs inserts or replaces the admin's "View as…"
	SaveViewAs(v models.ViewAs)
	// DeleteViewAs ends the admin's "View as…"
	DeleteViewAs(adminId int)

	// Part 4, K: blocklist
	// SaveBlock adds a blocklist entry (an email or "@domain"); an existing one is kept
	SaveBlock(b models.BlockEntry)
	// DeleteBlock removes a blocklist entry
	DeleteBlock(entry string)
	// IsBlocked returns true if the exact entry is on the blocklist
	IsBlocked(entry string) bool
	// GetAllBlocks returns every blocklist entry
	GetAllBlocks() []models.BlockEntry

	// GetStatTraffic returns the total traffic from statistics
	GetStatTraffic() uint64
	// SaveStatTraffic stores the total traffic
	SaveStatTraffic(totalTraffic uint64)
	// SaveTrafficSince stores the beginning of traffic counting
	SaveTrafficSince(since int64)
	// GetTrafficSince gets the beginning of traffic counting
	GetTrafficSince() (int64, bool)
}

// GetNew connects to the given database and initialises it
func GetNew(config models.DbConnection) (Database, error) {
	switch config.Type {
	case TypeSqlite:
		return sqlite.New(config)
	case TypeRedis:
		return redis.New(config)
	default:
		return nil, fmt.Errorf("unsupported database: type %v", config.Type)
	}
}
