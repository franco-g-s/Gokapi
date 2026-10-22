package redis

import "github.com/forceu/gokapi/internal/models"

// "View as…" needs the SQLite database, like groups. Redis has no ViewAs
// table: every call is a no-op and nothing is ever active. These must not
// panic, because the startup sweep and the ticker call them on every start.

func (p DatabaseProvider) GetViewAs(adminId int) (models.ViewAs, bool) { return models.ViewAs{}, false }
func (p DatabaseProvider) GetAllViewAs() []models.ViewAs               { return []models.ViewAs{} }
func (p DatabaseProvider) SaveViewAs(v models.ViewAs)                  {}
func (p DatabaseProvider) DeleteViewAs(adminId int)                    {}
