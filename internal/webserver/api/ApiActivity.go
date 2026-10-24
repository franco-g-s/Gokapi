package api

import (
	"encoding/json"
	"net/http"

	"github.com/forceu/gokapi/internal/activity"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
)

// apiActivityGet answers GET /api/activity (Part 4, A). An absent since
// header is "start from now", which is not the same as since: 0.
func apiActivityGet(w http.ResponseWriter, r requestParser, _ models.User, _ models.ApiKey) {
	request, ok := r.(*paramActivityGet)
	if !ok {
		panic("invalid parameter passed")
	}
	var since *int64
	if request.foundHeaders["since"] {
		since = &request.Since
	}
	limit := activity.DefaultLimit
	if request.foundHeaders["limit"] {
		limit = request.Limit
	}
	result, err := json.Marshal(activity.Feed(since, limit))
	helper.Check(err)
	_, _ = w.Write(result)
}
