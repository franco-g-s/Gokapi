package redis

import (
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const prefixPreviews = "prev:"

func dbToPreview(input []any) (models.Preview, error) {
	var result models.Preview
	err := redigo.ScanStruct(input, &result)
	if err != nil {
		return models.Preview{}, err
	}
	return result, nil
}

// GetPreview returns the preview row for a blob hash
func (p DatabaseProvider) GetPreview(sha1 string) (models.Preview, bool) {
	if sha1 == "" {
		return models.Preview{}, false
	}
	result, ok := p.getHashMap(prefixPreviews + sha1)
	if !ok {
		return models.Preview{}, false
	}
	preview, err := dbToPreview(result)
	helper.Check(err)
	return preview, true
}

// SavePreview inserts or replaces a preview row
func (p DatabaseProvider) SavePreview(pr models.Preview) {
	// HMSET only overwrites the fields it is given, and the struct always supplies all of them
	p.setHashMap(p.buildArgs(prefixPreviews + pr.Sha1).AddFlat(pr))
}

// DeletePreview removes a preview row
func (p DatabaseProvider) DeletePreview(sha1 string) {
	if sha1 == "" {
		return
	}
	p.deleteKey(prefixPreviews + sha1)
}

// GetAllPreviewSha1s maps every known blob hash to its preview status
func (p DatabaseProvider) GetAllPreviewSha1s() map[string]string {
	result := make(map[string]string)
	for _, v := range p.getAllHashesWithPrefix(prefixPreviews) {
		preview, err := dbToPreview(v)
		helper.Check(err)
		result[preview.Sha1] = preview.Status
	}
	return result
}
