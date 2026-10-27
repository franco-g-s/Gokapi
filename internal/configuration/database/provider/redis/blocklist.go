package redis

import "github.com/forceu/gokapi/internal/models"

// The blocklist needs the SQLite database, like groups: Redis blocks nobody.

func (p DatabaseProvider) SaveBlock(b models.BlockEntry)     {}
func (p DatabaseProvider) DeleteBlock(entry string)          {}
func (p DatabaseProvider) IsBlocked(entry string) bool       { return false }
func (p DatabaseProvider) GetAllBlocks() []models.BlockEntry { return []models.BlockEntry{} }
