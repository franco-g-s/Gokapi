package webserver

// List batching (Part 3, T1): a page renders the first listBatch items of
// its list, and its JSON feed answers the later batches. Filters and sort run
// on the server, over the same browserItems the page renders, and a keyset
// cursor carries the reader from one batch to the next.

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/preview"
)

// listBatch is how many items a page renders and a feed answers by default
const listBatch = 100

// listMaxLimit is the largest numeric limit= a feed accepts
const listMaxLimit = 500

var (
	errListSort   = errors.New("unknown sort")
	errListLimit  = errors.New("limit must be 1 to 500, or all")
	errListScope  = errors.New("unknown scope")
	errListPerson = errors.New("invalid person")
	errListCursor = errors.New("invalid cursor")
	errListKind   = errors.New("unknown kind")
	// errListCursorSort: the list was re-sorted since the cursor was made
	errListCursorSort = errors.New("cursor does not match the sort")
)

// listSorts are the sort= values a feed accepts
var listSorts = []string{"name", "uploaded", "taken", "size", "expires", "downloads"}

// listKinds are the kind= values a feed accepts (the Type filter)
var listKinds = []string{"image", "video", "other"}

// listQuery is one request for a batch of a list
type listQuery struct {
	Person  int    // person=<user id>; 0 = everyone (group pages only)
	HideHad bool   // hide=1: only items new to the viewer (D12)
	NewOnly bool   // new=1: the same predicate, for "Select everything new to me"
	Scope   string // scope=mine|all (Files tab switch); "" = the source's default
	Search  string // q=: case-insensitive substring of the file name
	Kind    string // kind=image|video|other (Type); "" = every kind
	Sort    string // sort=name|uploaded|taken|size|expires|downloads; "" = the source's default
	Desc    bool   // dir=desc; ignored when Sort is ""
	After   string // after=<cursor> from the previous answer's Next
	Limit   int    // limit=<n> (1..500); limit=all → -1 (resolving a selection); default listBatch
}

// parseListQuery reads a feed's query string. An unknown sort or scope, a
// bad person or a limit outside 1..500 (other than "all") is an error.
func parseListQuery(r *http.Request) (listQuery, error) {
	values := r.URL.Query()
	q := listQuery{
		HideHad: values.Get("hide") == "1",
		NewOnly: values.Get("new") == "1",
		Scope:   values.Get("scope"),
		Search:  values.Get("q"),
		Kind:    values.Get("kind"),
		Sort:    values.Get("sort"),
		Desc:    values.Get("dir") == "desc",
		After:   values.Get("after"),
		Limit:   listBatch,
	}
	if raw := values.Get("person"); raw != "" {
		person, err := strconv.Atoi(raw)
		if err != nil || person < 0 {
			return listQuery{}, errListPerson
		}
		q.Person = person
	}
	if q.Scope != "" && q.Scope != "mine" && q.Scope != "all" {
		return listQuery{}, errListScope
	}
	if q.Sort != "" && !slices.Contains(listSorts, q.Sort) {
		return listQuery{}, errListSort
	}
	if q.Kind != "" && !slices.Contains(listKinds, q.Kind) {
		return listQuery{}, errListKind
	}
	if raw := values.Get("limit"); raw == "all" {
		q.Limit = -1
	} else if raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > listMaxLimit {
			return listQuery{}, errListLimit
		}
		q.Limit = limit
	}
	return q, nil
}

// listSource is one page's whole list and how it is filtered by default
type listSource struct {
	Items        []browserItem // the whole list, unfiltered
	DefaultSort  string        // "uploaded" (desc) or "taken" (asc, group pages, D13)
	DefaultDesc  bool
	ViewerId     int    // for Mine / new-to-me
	HasScope     bool   // the Files tab's My files / Everything switch applies
	DefaultScope string // "mine" where HasScope (the client's default), else ""
	GroupTools   bool   // person, hide and new apply, and New is counted
}

// order is the sort and direction a query is answered in: its own, or the
// source's default when it names none
func (s listSource) order(q listQuery) (string, bool) {
	if q.Sort == "" {
		return s.DefaultSort, s.DefaultDesc
	}
	return q.Sort, q.Desc
}

// newToViewer is the D12 predicate: neither downloaded nor uploaded by them
func newToViewer(item browserItem) bool { return !item.Downloaded && !item.Mine }

// kindOf is an item's Type: its preview's kind, else its extension's (no
// preview yet, or none), else "other". media.js (FGS.fileKind, behind
// FGS.isPhotoOrVideo) holds the same rule.
func kindOf(item browserItem) string {
	switch item.Preview.Kind {
	case models.PreviewKindImage, models.PreviewKindVideo:
		return item.Preview.Kind
	}
	if kind := preview.Kind("", item.File.Name); kind != "" {
		return kind
	}
	return "other"
}

// passes reports whether an item survives the query's filters; withNew
// false leaves out hide and new, for counting New
func (s listSource) passes(item browserItem, q listQuery, withNew bool) bool {
	if s.HasScope {
		scope := q.Scope
		if scope == "" {
			scope = s.DefaultScope
		}
		if scope == "mine" && !item.Mine {
			return false
		}
	}
	if s.GroupTools {
		if q.Person > 0 && item.UploaderId != q.Person {
			return false
		}
		if withNew && (q.HideHad || q.NewOnly) && !newToViewer(item) {
			return false
		}
	}
	if q.Kind != "" && kindOf(item) != q.Kind {
		return false
	}
	if search := strings.ToLower(strings.TrimSpace(q.Search)); search != "" &&
		!strings.Contains(strings.ToLower(item.File.Name), search) {
		return false
	}
	return true
}

// listKey is an item's position in a sort: n for the numeric keys, s for
// name, and the file id as the tie-break
type listKey struct {
	n  int64
	s  string
	id string
}

// listSortKey is an item's key under a sort
func listSortKey(item browserItem, sort string) listKey {
	key := listKey{id: item.File.Id}
	switch sort {
	case "name":
		key.s = strings.ToLower(item.File.Name)
	case "uploaded":
		key.n = item.File.UploadDate
	case "taken":
		key.n = shootingKey(item)
	case "size":
		key.n = item.Bytes
	case "expires":
		switch {
		case item.Foreign:
			key.n = 0
		case item.File.UnlimitedTime:
			key.n = math.MaxInt64
		default:
			key.n = item.File.ExpireAt
		}
	case "downloads":
		key.n = int64(item.File.DownloadCount)
	}
	return key
}

// compareListKeys orders two keys of one sort, ascending unless desc. The
// file id breaks ties in the same direction, so the order is total.
func compareListKeys(a, b listKey, sort string, desc bool) int {
	var result int
	if sort == "name" {
		result = cmp.Compare(a.s, b.s)
	} else {
		result = cmp.Compare(a.n, b.n)
	}
	if result == 0 {
		result = cmp.Compare(a.id, b.id)
	}
	if desc {
		return -result
	}
	return result
}

// matching filters and sorts; total order (key, id) so the cursor is exact
func (s listSource) matching(q listQuery) []browserItem {
	sort, desc := s.order(q)
	result := make([]browserItem, 0, len(s.Items))
	for _, item := range s.Items {
		if s.passes(item, q, true) {
			result = append(result, item)
		}
	}
	slices.SortFunc(result, func(a, b browserItem) int {
		return compareListKeys(listSortKey(a, sort), listSortKey(b, sort), sort, desc)
	})
	return result
}

// newCount is how many items pass every filter but hide and new and are
// new to the viewer; 0 without the group tools
func (s listSource) newCount(q listQuery) int {
	if !s.GroupTools {
		return 0
	}
	count := 0
	for _, item := range s.Items {
		if newToViewer(item) && s.passes(item, q, false) {
			count++
		}
	}
	return count
}

// listCursor is the JSON inside a cursor
type listCursor struct {
	Sort string `json:"s"`
	Desc bool   `json:"d"`
	Key  string `json:"k"`
	Id   string `json:"i"`
}

// encodeListCursor makes the cursor after an item
func encodeListCursor(item browserItem, sort string, desc bool) string {
	key := listSortKey(item, sort)
	c := listCursor{Sort: sort, Desc: desc, Key: key.s, Id: key.id}
	if sort != "name" {
		c.Key = strconv.FormatInt(key.n, 10)
	}
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeListCursor reads a cursor made under this sort and direction
func decodeListCursor(cursor, sort string, desc bool) (listKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return listKey{}, errListCursor
	}
	var c listCursor
	if json.Unmarshal(raw, &c) != nil {
		return listKey{}, errListCursor
	}
	if c.Sort != sort || c.Desc != desc {
		return listKey{}, errListCursorSort
	}
	key := listKey{s: c.Key, id: c.Id}
	if sort != "name" {
		key.s = ""
		key.n, err = strconv.ParseInt(c.Key, 10, 64)
		if err != nil {
			return listKey{}, errListCursor
		}
	}
	return key, nil
}

// pageAfter returns up to q.Limit items after q.After, and the cursor of the
// last one returned ("" when nothing follows). The cursor is a position in
// the order, not an index: an item that left the list since (downloaded
// under "Hide what I've downloaded") skips nothing.
func (s listSource) pageAfter(matching []browserItem, q listQuery) ([]browserItem, string, error) {
	sort, desc := s.order(q)
	start := 0
	if q.After != "" {
		after, err := decodeListCursor(q.After, sort, desc)
		if err != nil {
			return nil, "", err
		}
		start, _ = slices.BinarySearchFunc(matching, after, func(item browserItem, target listKey) int {
			if compareListKeys(listSortKey(item, sort), target, sort, desc) <= 0 {
				return -1
			}
			return 1
		})
	}
	end := len(matching)
	if q.Limit >= 0 {
		end = min(start+q.Limit, len(matching))
	}
	page := matching[start:end]
	next := ""
	if end < len(matching) && end > start {
		next = encodeListCursor(matching[end-1], sort, desc)
	}
	return page, next, nil
}

// listItemJson is one item of a feed: everything its tile renders
type listItemJson struct {
	models.FileApiOutput // already blanked for Foreign items (makeForeign)
	// UploaderId shadows FileApiOutput's: browserItem.UploaderId (group
	// items), else the file's own. collectionPublicSource zeroes the file's,
	// so a public collection never learns who uploaded what.
	UploaderId   int
	UploaderName string
	Mine         bool
	Downloaded   bool
	CanEdit      bool
	CanDelete    bool
	CanReplace   bool
	CanRemove    bool
	Foreign      bool
	GroupLock    string
	Download     string // browserItem.DownloadUrl
	Bytes        int64
	TakenAt      int64
	Preview      previewInfo
}

// listItemOf is the feed's form of an item
func listItemOf(item browserItem) listItemJson {
	return listItemJson{
		FileApiOutput: item.File,
		UploaderId:    cmp.Or(item.UploaderId, item.File.UploaderId),
		UploaderName:  item.Uploader,
		Mine:          item.Mine,
		Downloaded:    item.Downloaded,
		CanEdit:       item.CanEdit,
		CanDelete:     item.CanDelete,
		CanReplace:    item.CanReplace,
		CanRemove:     item.CanRemove,
		Foreign:       item.Foreign,
		GroupLock:     item.GroupLock,
		Download:      item.DownloadUrl,
		Bytes:         item.Bytes,
		TakenAt:       item.TakenAt,
		Preview:       item.Preview,
	}
}

// listAnswer is a feed's answer
type listAnswer struct {
	Result string // "OK"
	Total  int    // items matching the query's filters (not the cursor)
	All    int    // the whole list
	New    int    // GroupTools: matching items new to the viewer; else 0
	Items  []listItemJson
	Next   string // "" = this was the last batch
}

// listBatchOf is one batch of the source under a query, with its counts
type listBatchOf struct {
	Items []browserItem
	Next  string
	Total int
	All   int
	New   int
}

// batch answers a query: the page after its cursor and the counts
func (s listSource) batch(q listQuery) (listBatchOf, error) {
	matching := s.matching(q)
	page, next, err := s.pageAfter(matching, q)
	if err != nil {
		return listBatchOf{}, err
	}
	return listBatchOf{Items: page, Next: next, Total: len(matching), All: len(s.Items), New: s.newCount(q)}, nil
}

// answer is batch in the feed's JSON form
func (s listSource) answer(q listQuery) (listAnswer, error) {
	b, err := s.batch(q)
	if err != nil {
		return listAnswer{}, err
	}
	items := make([]listItemJson, 0, len(b.Items))
	for _, item := range b.Items {
		items = append(items, listItemOf(item))
	}
	return listAnswer{Result: "OK", Total: b.Total, All: b.All, New: b.New, Items: items, Next: b.Next}, nil
}

// writeList parses the request's query and answers one batch as uncached
// JSON; a bad query is a 400
func writeList(w http.ResponseWriter, r *http.Request, s listSource) {
	q, err := parseListQuery(r)
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, err)
		return
	}
	answer, err := s.answer(q)
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, err)
		return
	}
	writeJson(w, answer)
}
