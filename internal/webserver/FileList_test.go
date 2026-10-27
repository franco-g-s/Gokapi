//go:build !integration && test

package webserver

import (
	"fmt"
	"math"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func listIds(items []browserItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.File.Id)
	}
	return result
}

func mustParse(t *testing.T, query string) listQuery {
	t.Helper()
	q, err := parseListQuery(httptest.NewRequest("GET", "/gl?"+query, nil))
	test.IsNil(t, err)
	return q
}

func TestListQueryParse(t *testing.T) {
	q := mustParse(t, "")
	test.IsEqualInt(t, q.Limit, listBatch)
	test.IsEqualInt(t, listBatch, 100)
	test.IsEqualString(t, q.Sort, "")
	test.IsEqualBool(t, q.Desc, false)

	q = mustParse(t, "limit=all")
	test.IsEqualInt(t, q.Limit, -1)
	q = mustParse(t, "limit=500&sort=size&dir=desc&person=7&hide=1&new=1&scope=all&q=%20Fjord%20&after=abc")
	test.IsEqualInt(t, q.Limit, 500)
	test.IsEqualString(t, q.Sort, "size")
	test.IsEqualBool(t, q.Desc, true)
	test.IsEqualInt(t, q.Person, 7)
	test.IsEqualBool(t, q.HideHad, true)
	test.IsEqualBool(t, q.NewOnly, true)
	test.IsEqualString(t, q.Scope, "all")
	test.IsEqualString(t, q.Search, " Fjord ")
	test.IsEqualString(t, q.After, "abc")
	q = mustParse(t, "limit=1")
	test.IsEqualInt(t, q.Limit, 1)

	for _, kind := range []string{"image", "video", "other"} {
		test.IsEqualString(t, mustParse(t, "kind="+kind).Kind, kind)
	}
	test.IsEqualString(t, mustParse(t, "").Kind, "")

	for _, bad := range []string{"limit=0", "limit=501", "limit=-1", "limit=x", "sort=bogus", "kind=bogus", "kind=Image"} {
		_, err := parseListQuery(httptest.NewRequest("GET", "/gl?"+bad, nil))
		test.IsNotNil(t, err)
	}
	for _, sortKey := range []string{"name", "uploaded", "taken", "size", "expires", "downloads"} {
		test.IsEqualString(t, mustParse(t, "sort="+sortKey).Sort, sortKey)
	}
}

// listFixture: mine (viewer 1's), had (someone else's, downloaded), fresh
// (someone else's, new to the viewer) and other (person 3's, new)
func listFixture(groupTools, hasScope bool) listSource {
	item := func(id, name string, uploader int, mine, downloaded bool) browserItem {
		return browserItem{File: models.FileApiOutput{Id: id, Name: name, UploadDate: 100},
			UploaderId: uploader, Mine: mine, Downloaded: downloaded}
	}
	return listSource{
		Items: []browserItem{
			item("mine", "Fjord.jpg", 1, true, false),
			item("had", "glacier.JPG", 2, false, true),
			item("fresh", "puffin.png", 2, false, false),
			item("other", "fjord-2.mov", 3, false, false),
		},
		DefaultSort: "uploaded", DefaultDesc: true, ViewerId: 1,
		HasScope: hasScope, DefaultScope: map[bool]string{true: "mine", false: ""}[hasScope],
		GroupTools: groupTools,
	}
}

func matchingIds(s listSource, q listQuery) []string {
	ids := listIds(s.matching(q))
	slices.Sort(ids)
	return ids
}

func sameIds(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestListMatchingFilters(t *testing.T) {
	group := listFixture(true, false)
	sameIds(t, matchingIds(group, listQuery{}), "mine", "had", "fresh", "other")
	sameIds(t, matchingIds(group, listQuery{Person: 2}), "had", "fresh")
	sameIds(t, matchingIds(group, listQuery{HideHad: true}), "fresh", "other")
	sameIds(t, matchingIds(group, listQuery{NewOnly: true}), "fresh", "other")
	sameIds(t, matchingIds(group, listQuery{Person: 2, HideHad: true}), "fresh")
	sameIds(t, matchingIds(group, listQuery{Search: "  FJORD "}), "mine", "other")
	sameIds(t, matchingIds(group, listQuery{Search: "jpg"}), "mine", "had")
	// No switch on a group page: scope is ignored
	sameIds(t, matchingIds(group, listQuery{Scope: "mine"}), "mine", "had", "fresh", "other")

	// Person, hide and new apply only where the group tools are
	plain := listFixture(false, false)
	sameIds(t, matchingIds(plain, listQuery{Person: 2, HideHad: true, NewOnly: true}), "mine", "had", "fresh", "other")

	// The Files tab with its switch: mine by default, everything on asking
	files := listFixture(false, true)
	sameIds(t, matchingIds(files, listQuery{}), "mine")
	sameIds(t, matchingIds(files, listQuery{Scope: "mine"}), "mine")
	sameIds(t, matchingIds(files, listQuery{Scope: "all"}), "mine", "had", "fresh", "other")
	sameIds(t, matchingIds(files, listQuery{Scope: "all", Search: "fjord"}), "mine", "other")

	// Type (R1): the preview's kind, else the extension, as FGS.isPhotoOrVideo
	sameIds(t, matchingIds(group, listQuery{Kind: "image"}), "mine", "had", "fresh")
	sameIds(t, matchingIds(group, listQuery{Kind: "video"}), "other")
	sameIds(t, matchingIds(group, listQuery{Kind: "other"}))
	sameIds(t, matchingIds(files, listQuery{Scope: "all", Kind: "video"}), "other")
	sameIds(t, matchingIds(group, listQuery{Kind: "image", Person: 2}), "had", "fresh")
	kinds := listSource{Items: []browserItem{
		{File: models.FileApiOutput{Id: "clip", Name: "clip.bin"}, Preview: previewInfo{Kind: "video"}},
		{File: models.FileApiOutput{Id: "pic", Name: "scan.bin"}, Preview: previewInfo{Kind: "image", Status: "pending"}},
		{File: models.FileApiOutput{Id: "heic", Name: "IMG_1.HEIC"}},
		{File: models.FileApiOutput{Id: "m4v", Name: "a.M4V"}},
		{File: models.FileApiOutput{Id: "txt", Name: "notes.txt"}},
		{File: models.FileApiOutput{Id: "svg", Name: "logo.svg"}},
		{File: models.FileApiOutput{Id: "bare", Name: "README"}},
	}, DefaultSort: "uploaded", DefaultDesc: true, GroupTools: true}
	sameIds(t, matchingIds(kinds, listQuery{Kind: "image"}), "pic", "heic")
	sameIds(t, matchingIds(kinds, listQuery{Kind: "video"}), "clip", "m4v")
	sameIds(t, matchingIds(kinds, listQuery{Kind: "other"}), "txt", "svg", "bare")
	sameIds(t, matchingIds(kinds, listQuery{}), "clip", "pic", "heic", "m4v", "txt", "svg", "bare")
	// New counts under the Type filter too
	test.IsEqualInt(t, kinds.newCount(listQuery{Kind: "video"}), 2)
	test.IsEqualInt(t, group.newCount(listQuery{Kind: "image"}), 1)
}

// sortFixture has two items that tie on every key, so the id decides
func sortFixture() listSource {
	return listSource{Items: []browserItem{
		{File: models.FileApiOutput{Id: "b", Name: "Same", UploadDate: 10, ExpireAt: 50, DownloadCount: 4}, Bytes: 7, TakenAt: 0},
		{File: models.FileApiOutput{Id: "a", Name: "same", UploadDate: 10, ExpireAt: 50, DownloadCount: 4}, Bytes: 7, TakenAt: 10},
		{File: models.FileApiOutput{Id: "c", Name: "apple", UploadDate: 5, ExpireAt: 90, DownloadCount: 9}, Bytes: 100, TakenAt: 30},
		{File: models.FileApiOutput{Id: "d", Name: "Zebra", UploadDate: 20, UnlimitedTime: true, DownloadCount: 1}, Bytes: 1, TakenAt: 2},
		{File: models.FileApiOutput{Id: "e", Name: "mango", UploadDate: 1}, Bytes: 3, Foreign: true},
	}, DefaultSort: "uploaded", DefaultDesc: true}
}

func TestListSortEveryKey(t *testing.T) {
	s := sortFixture()
	// name: apple, mango, same (a), Same (b), Zebra. taken: b falls back to
	// its upload date 10. expires: Foreign is 0, never last.
	ascending := map[string][]string{
		"name":      {"c", "e", "a", "b", "d"},
		"uploaded":  {"e", "c", "a", "b", "d"},
		"taken":     {"e", "d", "a", "b", "c"},
		"size":      {"d", "e", "a", "b", "c"},
		"expires":   {"e", "a", "b", "c", "d"},
		"downloads": {"e", "d", "a", "b", "c"},
	}
	for key, want := range ascending {
		got := listIds(s.matching(listQuery{Sort: key}))
		if !slices.Equal(got, want) {
			t.Fatalf("%s asc: got %v, want %v", key, got, want)
		}
		reversed := slices.Clone(want)
		slices.Reverse(reversed)
		got = listIds(s.matching(listQuery{Sort: key, Desc: true}))
		if !slices.Equal(got, reversed) {
			t.Fatalf("%s desc: got %v, want %v", key, got, reversed)
		}
	}
	// "" is the source's default: uploaded, newest first; Desc is ignored
	test.IsEqualString(t, fmt.Sprint(listIds(s.matching(listQuery{Desc: false}))), "[d b a c e]")
	group := sortFixture()
	group.DefaultSort, group.DefaultDesc = "taken", false
	test.IsEqualString(t, fmt.Sprint(listIds(group.matching(listQuery{Desc: true}))), "[e d a b c]")
	test.IsEqualBool(t, listSortKey(browserItem{File: models.FileApiOutput{UnlimitedTime: true}}, "expires").n == math.MaxInt64, true)
}

func bigSource(n int) listSource {
	items := make([]browserItem, 0, n)
	for i := 0; i < n; i++ {
		// Upload dates repeat, so the id tie-break is exercised on every page
		items = append(items, browserItem{File: models.FileApiOutput{
			Id: fmt.Sprintf("f%04d", i), Name: fmt.Sprintf("n%04d.jpg", i), UploadDate: int64(i / 3)}})
	}
	return listSource{Items: items, DefaultSort: "uploaded", DefaultDesc: true, GroupTools: true}
}

func TestListCursorWalk(t *testing.T) {
	s := bigSource(250)
	q := listQuery{Limit: listBatch}
	all := s.matching(q)
	test.IsEqualInt(t, len(all), 250)
	seen := make([]string, 0, 250)
	pages := 0
	for {
		page, next, err := s.pageAfter(s.matching(q), q)
		test.IsNil(t, err)
		pages++
		seen = append(seen, listIds(page)...)
		if next == "" {
			test.IsEqualInt(t, len(page), 50)
			break
		}
		test.IsEqualInt(t, len(page), 100)
		q.After = next
	}
	test.IsEqualInt(t, pages, 3)
	if !slices.Equal(seen, listIds(all)) {
		t.Fatal("the pages do not add up to the sorted list exactly once")
	}
	// limit=all after a cursor: the rest
	first, next, err := s.pageAfter(all, listQuery{Limit: listBatch})
	test.IsNil(t, err)
	rest, last, err := s.pageAfter(all, listQuery{Limit: -1, After: next})
	test.IsNil(t, err)
	test.IsEqualInt(t, len(first)+len(rest), 250)
	test.IsEqualString(t, last, "")
	// a garbled cursor is refused
	_, _, err = s.pageAfter(all, listQuery{Limit: 10, After: "!!not-a-cursor"})
	test.IsNotNil(t, err)
}

func TestListCursorStableUnderHide(t *testing.T) {
	s := bigSource(250)
	q := listQuery{Limit: listBatch, HideHad: true}
	before := s.matching(q)
	page1, next, err := s.pageAfter(before, q)
	test.IsNil(t, err)
	// Two items of page 1 are downloaded while the reader scrolls; the last
	// one of page 1 too, so the cursor's own item has left the list
	byId := make(map[string]int)
	for i, item := range s.Items {
		byId[item.File.Id] = i
	}
	for _, i := range []int{3, 7, 99} {
		s.Items[byId[page1[i].File.Id]].Downloaded = true
	}
	q.After = next
	page2, _, err := s.pageAfter(s.matching(q), q)
	test.IsNil(t, err)
	test.IsEqualString(t, page2[0].File.Id, before[100].File.Id)
	test.IsEqualInt(t, len(page2), 100)
	if !slices.Equal(listIds(page2), listIds(before[100:200])) {
		t.Fatal("page 2 skipped or repeated an item")
	}
}

func TestListCursorMismatch(t *testing.T) {
	s := bigSource(20)
	byName := listQuery{Sort: "name", Limit: 5}
	_, next, err := s.pageAfter(s.matching(byName), byName)
	test.IsNil(t, err)
	bySize := listQuery{Sort: "size", Limit: 5, After: next}
	_, _, err = s.pageAfter(s.matching(bySize), bySize)
	test.IsNotNil(t, err)
	// the same sort in the other direction is a mismatch too
	desc := listQuery{Sort: "name", Desc: true, Limit: 5, After: next}
	_, _, err = s.pageAfter(s.matching(desc), desc)
	test.IsNotNil(t, err)
	// a cursor from the default query works with sort= left out
	def := listQuery{Limit: 5}
	_, next, err = s.pageAfter(s.matching(def), def)
	test.IsNil(t, err)
	def.After = next
	_, _, err = s.pageAfter(s.matching(def), def)
	test.IsNil(t, err)
}

func TestListCounts(t *testing.T) {
	s := listFixture(true, false)
	s.Items = append(s.Items, browserItem{File: models.FileApiOutput{Id: "fresh2", Name: "x"}, UploaderId: 2})
	answer, err := s.answer(listQuery{Person: 2, HideHad: true, Limit: listBatch})
	test.IsNil(t, err)
	test.IsEqualInt(t, answer.Total, 2) // fresh, fresh2
	test.IsEqualInt(t, answer.All, 5)
	test.IsEqualInt(t, answer.New, 2)
	test.IsEqualString(t, answer.Result, "OK")
	test.IsEqualString(t, answer.Next, "")
	// New ignores the hide filter but keeps the others
	answer, err = s.answer(listQuery{Person: 2, Limit: listBatch})
	test.IsNil(t, err)
	test.IsEqualInt(t, answer.Total, 3) // had, fresh, fresh2
	test.IsEqualInt(t, answer.New, 2)
	answer, err = s.answer(listQuery{Limit: 1})
	test.IsNil(t, err)
	test.IsEqualInt(t, answer.Total, 5)
	test.IsEqualInt(t, answer.New, 3)
	test.IsEqualInt(t, len(answer.Items), 1)
	test.IsEqualBool(t, answer.Next != "", true)
	// Without the group tools New is 0
	plain := listFixture(false, false)
	answer, err = plain.answer(listQuery{Limit: listBatch})
	test.IsNil(t, err)
	test.IsEqualInt(t, answer.New, 0)
}
