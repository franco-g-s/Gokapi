package redis

import "github.com/forceu/gokapi/internal/models"

// Groups need the SQLite database (D15). Redis has no group tables, so every
// read returns its zero value and every group write panics: those writes are
// unreachable, because storage checks SupportsGroups() first and answers
// ErrGroupsUnsupported. SaveDownload, SaveCollectionSeen and
// DeleteDownloadsOfFile and CopyDownloads are no-ops instead, so a download never breaks on Redis.

const groupsUnsupported = "groups need the SQLite database"

// SupportsGroups is false: Redis has no group tables
func (p DatabaseProvider) SupportsGroups() bool { return false }

func (p DatabaseProvider) GetGroup(id string) (models.Group, bool) { return models.Group{}, false }
func (p DatabaseProvider) GetGroupByToken(token string) (models.Group, bool) {
	return models.Group{}, false
}
func (p DatabaseProvider) GetGroupByCode(code string) (models.Group, bool) {
	return models.Group{}, false
}
func (p DatabaseProvider) GetAllGroups() []models.Group { return []models.Group{} }
func (p DatabaseProvider) SaveGroup(g models.Group)     { panic(groupsUnsupported) }
func (p DatabaseProvider) DeleteGroup(id string)        { panic(groupsUnsupported) }
func (p DatabaseProvider) GetGroupMembers(groupId string) []models.GroupMember {
	return []models.GroupMember{}
}
func (p DatabaseProvider) GetMembershipsOfUser(userId int) []models.GroupMember {
	return []models.GroupMember{}
}
func (p DatabaseProvider) SaveGroupMember(m models.GroupMember)             { panic(groupsUnsupported) }
func (p DatabaseProvider) DeleteGroupMember(groupId string, userId int)     { panic(groupsUnsupported) }
func (p DatabaseProvider) GetGroupFileIds(groupId string) []string          { return []string{} }
func (p DatabaseProvider) GetGroupIdsOfFile(fileId string) []string         { return []string{} }
func (p DatabaseProvider) AddGroupFile(groupId, fileId string)              { panic(groupsUnsupported) }
func (p DatabaseProvider) RemoveGroupFile(groupId, fileId string)           { panic(groupsUnsupported) }
func (p DatabaseProvider) RemoveFileFromGroups(fileId string)               {}
func (p DatabaseProvider) SaveNearCopyKept(groupId, fileId string)          { panic(groupsUnsupported) }
func (p DatabaseProvider) GetNearCopiesKept(groupId string) map[string]bool { return map[string]bool{} }
func (p DatabaseProvider) SaveDownload(d models.Download)                   {}
func (p DatabaseProvider) GetDownloadedFileIds(userId int) map[string]bool  { return map[string]bool{} }
func (p DatabaseProvider) DeleteDownloadsOfFile(fileId string)              {}
func (p DatabaseProvider) CopyDownloads(fromFileId, toFileId string)        {}
func (p DatabaseProvider) SaveCollectionSeen(s models.CollectionSeen)       {}
func (p DatabaseProvider) GetCollectionsSeen(userId int) []models.CollectionSeen {
	return []models.CollectionSeen{}
}
func (p DatabaseProvider) GetCollectionViewers(collectionId string) []models.CollectionSeen {
	return []models.CollectionSeen{}
}
func (p DatabaseProvider) SaveCollectionGroup(cg models.CollectionGroup) { panic(groupsUnsupported) }
func (p DatabaseProvider) GetCollectionGroup(collectionId string) (models.CollectionGroup, bool) {
	return models.CollectionGroup{}, false
}
func (p DatabaseProvider) GetCollectionGroupsOfGroup(groupId string) []models.CollectionGroup {
	return []models.CollectionGroup{}
}
func (p DatabaseProvider) DeleteCollectionGroup(collectionId string) { panic(groupsUnsupported) }
