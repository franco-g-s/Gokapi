#!/usr/bin/env python3
"""End-to-end test of Part 3 (batching, filters, cursor, zip, View as…) against
a local Gokapi fork on 127.0.0.1:53900, set up by setup_local.py.

The fixture is build/e2e/seed_part3.py, run as a subprocess: users A and B, a
group G with A's 230 and B's 20 tiny PNGs (p3-<nnn>-<word>.png, the content of
file number nnn is seed_part3.png(nnn)), and a 150-file collection by A.

  2. batching     /g renders 100 tiles; /gl walks the other 150 in 2 batches
  3. parity       every rendered tile attribute equals the feed's JSON field
  4. filters      person, hide (D12), and a cursor that skips nothing when
                  items leave the list between two batches
  5. zip          /gz with all 250 ids; limit=all&person=A
  6. sort, search  server-side
  7. feed access  404 outside the group, 405 for a POST
  8. View as…     the admin sees B's site, read-only, and it is logged

Requests are made with urllib and one cookie jar per person; redirects are
never followed. Exit status 1 on any FAIL.
"""
import html, importlib.util, io, json, os, re, subprocess, sys, time, urllib.parse, zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("seed_part3", os.path.join(HERE, "seed_part3.py"))
seed = importlib.util.module_from_spec(spec)
spec.loader.exec_module(seed)
Client, png = seed.Client, seed.png

ok = True


def check(label, cond, detail=""):
    global ok
    if not cond:
        ok = False
    print("  %-72s %s %s" % (label, "PASS" if cond else "FAIL", detail))


def tile_tags(page):
    """The attribute dict of each real board tile (the <template> tile has no id)."""
    result = []
    for tag in re.findall(r'<figure class="fgs-tile"[^>]*>', page):
        attrs = {k: html.unescape(v) for k, v in re.findall(r'([\w-]+)="([^"]*)"', tag)}
        # a bare attribute (data-foreign="true" is rendered with a value, so none are bare)
        if attrs.get("data-id"):
            result.append(attrs)
    return result


def root_attrs(page):
    tag = re.search(r'<div class="fgs-browser"[^>]*>', page)
    return {k: html.unescape(v) for k, v in re.findall(r'([\w-]+)="([^"]*)"', tag.group(0))} if tag else {}


def feed(client, gid, **params):
    query = urllib.parse.urlencode(dict(params, id=gid))
    s, j = client.json("/gl?" + query)
    return j if s == 200 else {"Items": [], "Total": -1, "Next": "", "status": s}


def walk(client, gid, **params):
    """Follow Next from the first batch; returns the list of batches."""
    batches, after = [], ""
    while True:
        extra = dict(params, **({"after": after} if after else {}))
        batch = feed(client, gid, **extra)
        batches.append(batch)
        after = batch.get("Next", "")
        if not after or len(batches) > 20:
            return batches


def index_of(name):
    m = re.match(r"p3-(\d{3})-", name)
    return int(m.group(1)) if m else -1


def key_of(item):
    return item["TakenAt"] or item["UploadDate"]


# ---- 1. setup ---------------------------------------------------------------
print("1. setup: seed_part3.py (users A, B, group G, 230 + 20 files, a collection)")
t0 = time.time()
proc = subprocess.run([sys.executable, os.path.join(HERE, "seed_part3.py")],
                      stdout=subprocess.PIPE, stderr=sys.stderr, text=True)
check("the fixture ran", proc.returncode == 0, "exit %s in %.0fs" % (proc.returncode, time.time() - t0))
if proc.returncode != 0:
    sys.exit(1)
fx = json.loads(proc.stdout.strip().splitlines()[-1])
gid, cid = fx["group"], fx["collection"]
ids_a, ids_b = fx["ids"]["A"], fx["ids"]["B"]
all_ids = ids_a + ids_b
check("230 + 20 file ids, a group and a collection",
      len(ids_a) == 230 and len(ids_b) == 20 and len(set(all_ids)) == 250 and bool(gid) and bool(cid))

admin = Client("admin")
check("admin logs in", admin.login(seed.ADMIN, seed.ADMIN_PW) in (302, 303, 307))
A, B = Client(fx["A"]["name"]), Client(fx["B"]["name"])
A.id, B.id = fx["A"]["id"], fx["B"]["id"]
check("A and B sign in", A.login(A.name, fx["A"]["password"]) in (302, 303, 307)
      and B.login(B.name, fx["B"]["password"]) in (302, 303, 307))
outsider = seed.make_user(admin, "c")            # in no group
check("a third user, in no group, signs in", outsider.text("/groups")[0] == 200)

# ---- 2. batching ------------------------------------------------------------
print("2. batching: the page renders 100, the feed walks the rest")
s, page = B.text("/g?id=" + gid)
check("B: GET /g?id= is 200", s == 200)
root = root_attrs(page)
tiles = tile_tags(page)
check("the page renders exactly 100 tiles", len(tiles) == 100, str(len(tiles)))
check('data-total="250"', root.get("data-total") == "250", root.get("data-total", ""))
page_next = root.get("data-next", "")
check("data-next is non-empty", bool(page_next))
check("data-list-url points at the feed", root.get("data-list-url", "").startswith("./gl?id="), root.get("data-list-url", ""))

first = feed(B, gid)
check("/gl answers 100 items, Total 250, All 250", len(first["Items"]) == 100 and first["Total"] == 250
      and first["All"] == 250, "%s %s %s" % (len(first["Items"]), first["Total"], first["All"]))
check("the first batch's Next is the page's data-next", first["Next"] == page_next)
check("the page's tiles are the feed's first 100, in order",
      [t["data-id"] for t in tiles] == [i["Id"] for i in first["Items"]])
batches = walk(B, gid)
check("walking /gl gives 3 batches (100, 100, 50)", [len(b["Items"]) for b in batches] == [100, 100, 50],
      str([len(b["Items"]) for b in batches]))
check("the last batch has no Next", batches[-1]["Next"] == "" and all(b["Next"] for b in batches[:-1]))
walked = [i for b in batches for i in b["Items"]]
check("the union is 250 unique ids, exactly the uploaded ones",
      len(walked) == 250 and {i["Id"] for i in walked} == set(all_ids))
keys = [(key_of(i), i["Id"]) for i in walked]
check("it is in shooting order (taken, else uploaded; id breaks ties)", keys == sorted(keys))
once = feed(B, gid, limit="all")
check("limit=all answers the same order in one call",
      [i["Id"] for i in once["Items"]] == [i["Id"] for i in walked] and once["Next"] == "")
check("limit=501 and limit=0 are 400", B.get("/gl?id=%s&limit=501" % gid)[0] == 400
      and B.get("/gl?id=%s&limit=0" % gid)[0] == 400)

# ---- 3. server/template parity ----------------------------------------------
print("3. parity: wait for the previews, then compare page and feed")
deadline, pending = time.time() + 180, -1
while time.time() < deadline:
    pending = sum(1 for i in feed(B, gid, limit="all")["Items"] if i["Preview"]["Status"] != "ready")
    if pending == 0:
        break
    time.sleep(2)
check("every preview is ready", pending == 0, "%d not ready after %.0fs" % (pending, 180 - (deadline - time.time())))
s, page = B.text("/g?id=" + gid)
tiles = tile_tags(page)
items = feed(B, gid, limit=100)["Items"]
check("the page and the feed both give 100", len(tiles) == 100 and len(items) == 100)
bad = []
for t, j in zip(tiles, items):
    want = {
        "data-id": j["Id"], "data-name": j["Name"], "data-mine": str(j["Mine"]).lower(),
        "data-uploader-id": str(j["UploaderId"]), "data-downloaded": str(j["Downloaded"]).lower(),
        "data-download": j["Download"], "data-view": j["Preview"]["View"], "data-video": j["Preview"]["Video"],
        "data-kind": j["Preview"]["Kind"], "data-preview-status": j["Preview"]["Status"],
        "data-uploader": j["UploaderName"], "data-taken": str(j["TakenAt"]),
        "data-uploaded": str(j["UploadDate"]), "data-size": j["Size"], "data-bytes": str(j["Bytes"]),
        "data-w": str(j["Preview"]["Width"]), "data-h": str(j["Preview"]["Height"]),
    }
    for attr, value in want.items():
        if t.get(attr) != value:
            bad.append((j["Id"], attr, t.get(attr), value))
    if ("data-foreign" in t) != bool(j["Foreign"]):
        bad.append((j["Id"], "data-foreign", t.get("data-foreign"), j["Foreign"]))
check("all tile attributes equal the feed's fields (100 tiles x 18)", not bad, str(bad[:3]))
thumbs = re.findall(r'<figure class="fgs-tile".*?<img [^>]*?src="([^"]*)"', page, re.S)
check("each rendered thumbnail is the feed's Preview.Thumb",
      [html.unescape(u) for u in thumbs[1:101]] == [j["Preview"]["Thumb"] for j in items] or
      [html.unescape(u) for u in thumbs[:100]] == [j["Preview"]["Thumb"] for j in items],
      "%d imgs" % len(thumbs))
check("the items are the ones the page says: Foreign for A's, Mine for B's",
      all(j["Foreign"] == (j["UploaderId"] != B.id) and j["Mine"] == (j["UploaderId"] == B.id) for j in items))
check("I2: no /d link or counts leak into a foreign item",
      all(j["UrlDownload"] == "" and j["UrlHotlink"] == "" and j["DownloadCount"] == 0
          for j in items if j["Foreign"]))

# ---- 4. filters -------------------------------------------------------------
print("4. filters: person, hide, and a cursor that skips nothing")
r = feed(B, gid, person=A.id, limit="all")
check("person=A gives Total 230", r["Total"] == 230 and len(r["Items"]) == 230, str(r["Total"]))
r = feed(B, gid, person=B.id, limit=500)
check("person=B gives Total 20", r["Total"] == 20)
r = feed(B, gid, hide=1, limit=1)
check("hide=1 for B gives Total 230 (own uploads count as had, D12)", r["Total"] == 230 and r["New"] == 230,
      "Total %s New %s" % (r["Total"], r["New"]))
r = feed(B, gid, new=1, limit=1)
check("new=1 is the same predicate", r["Total"] == 230)
r = feed(A, gid, hide=1, limit=1)
check("hide=1 for A gives Total 20", r["Total"] == 20, str(r["Total"]))
check("bad person is 400", B.get("/gl?id=%s&person=x" % gid)[0] == 400)

index = {i["Id"]: n for n, i in enumerate(walked)}
five = [i["Id"] for i in walked if i["UploaderId"] == A.id][:5]
name_of = {i["Id"]: i["Name"] for i in walked}
for fid in five:
    s, body, _ = B.get("/gd?id=%s&file=%s" % (gid, fid))
    check("B downloads A's %s through /gd (200, the original)" % name_of[fid],
          s == 200 and body == png(index_of(name_of[fid])), "%s %dB" % (s, len(body)))
r = feed(B, gid, hide=1, limit=1)
check("after 5 downloads hide=1 gives 225", r["Total"] == 225, str(r["Total"]))
r = feed(B, gid, limit=250)
ticked = {i["Id"] for i in r["Items"] if i["Downloaded"]}
check("exactly those 5 tiles are ticked for B", ticked == set(five))
r = feed(A, gid, limit="all")
check("A's own ticks are untouched by B's downloads", not any(i["Downloaded"] for i in r["Items"]))

# the cursor walk: five items leave the list between batch 1 and batch 2
hidden_start = feed(B, gid, hide=1, limit="all")
expect_ids = [i["Id"] for i in hidden_start["Items"]]
b1 = feed(B, gid, hide=1)
leaving = [i["Id"] for i in b1["Items"] if i["UploaderId"] == A.id][:5]
for fid in leaving:
    B.get("/gd?id=%s&file=%s" % (gid, fid))
check("5 items of batch 1 are downloaded between the batches", len(leaving) == 5 and len(b1["Items"]) == 100)
rest, after = [], b1["Next"]
while after:
    b = feed(B, gid, hide=1, after=after)
    rest += b["Items"]
    after = b["Next"]
    last_total = b["Total"]
seen = [i["Id"] for i in b1["Items"]] + [i["Id"] for i in rest]
check("hide=1 cursor: nothing is skipped, nothing repeated", seen == expect_ids, "%d of %d" % (len(seen), len(expect_ids)))
check("the batches after the change report Total 220", last_total == 220, str(last_total))
r = feed(B, gid, hide=1, limit=1)
check("hide=1 now gives 220", r["Total"] == 220, str(r["Total"]))

# ---- 5. select all and zip --------------------------------------------------
print("5. select all and zip")
s, body, h = B.post_form("/gz?id=" + gid, {"files": ",".join(all_ids)})
check("POST /gz with all 250 ids is 200 and a zip", s == 200 and "zip" in h.get("Content-Type", "").lower(),
      "%s %s %dB" % (s, h.get("Content-Type", ""), len(body)))
zf = zipfile.ZipFile(io.BytesIO(body)) if s == 200 else None
entries = zf.namelist() if zf else []
check("the zip has 250 entries", len(entries) == 250, str(len(entries)))
sample = [e for e in entries if 0 <= index_of(os.path.basename(e)) < 250][::25][:10]
same = [zf.read(e) == png(index_of(os.path.basename(e))) for e in sample]
check("10 sampled entries are byte-equal to the originals", len(sample) == 10 and all(same), str(len(sample)))
check("the entry names are the file names, all distinct",
      sorted(os.path.basename(e) for e in entries) == sorted(i["Name"] for i in walked))
s, _, _ = B.post_json("/gz?id=" + gid, {"files": all_ids})
check("a zip request with a JSON body selects nothing it can use (no crash)", s in (200, 400, 404, 410, 415), str(s))
s, body, _ = B.post_form("/gz?id=" + gid, {"files": ""})
check("an empty selection is not 'everything'", not (s == 200 and len(body) > 1000), "%s %dB" % (s, len(body)))
r = feed(B, gid, person=A.id, limit="all")
check("limit=all&person=A lists 230 ids", len(r["Items"]) == 230 and len({i["Id"] for i in r["Items"]}) == 230
      and {i["Id"] for i in r["Items"]} == set(ids_a), str(len(r["Items"])))
r = feed(B, gid, hide=1, limit="all")
check("after the zip nothing is new to B any more", r["Total"] == 0, str(r["Total"]))

# ---- 6. sort and search -----------------------------------------------------
print("6. sort and search")
r = feed(B, gid, sort="name", dir="asc", limit="all")
names = [i["Name"].lower() for i in r["Items"]]
check("sort=name&dir=asc is ordered by lower-cased name", len(names) == 250 and names == sorted(names))
r = feed(B, gid, sort="name", dir="desc", limit="all")
names = [i["Name"].lower() for i in r["Items"]]
check("sort=name&dir=desc is the reverse", len(names) == 250 and names == sorted(names, reverse=True))
cursor_walk = [i["Id"] for b in walk(B, gid, sort="name", dir="asc") for i in b["Items"]]
check("a name-sorted cursor walk equals the one-call order",
      cursor_walk == [i["Id"] for i in feed(B, gid, sort="name", dir="asc", limit="all")["Items"]])
r = feed(B, gid, sort="size", dir="desc", limit="all")
sizes = [i["Bytes"] for i in r["Items"]]
check("sort=size&dir=desc is non-increasing", sizes == sorted(sizes, reverse=True))
check("an unknown sort is 400", B.get("/gl?id=%s&sort=colour" % gid)[0] == 400)
check("a cursor from another sort is 400", B.get("/gl?id=%s&sort=name&after=%s" % (gid, first["Next"]))[0] == 400)
word = "harbour"
expect = sorted(n for n in (i["Name"] for i in walked) if word in n.lower())
r = feed(B, gid, q=word, limit="all")
check("q=%s returns exactly the matching names (%d)" % (word, len(expect)),
      len(expect) == 63 and sorted(i["Name"] for i in r["Items"]) == expect and r["Total"] == len(expect),
      "%d / %d" % (len(r["Items"]), len(expect)))
r = feed(B, gid, q="  HARBOUR ", limit="all")
check("the search is case-insensitive and trimmed", r["Total"] == len(expect))
r = feed(B, gid, q="p3-007-", limit="all")
check("a unique substring finds one file", [i["Name"] for i in r["Items"]] == ["p3-007-summit.png"],
      str([i["Name"] for i in r["Items"]]))
r = feed(B, gid, q="no-such-name-anywhere", limit="all")
check("no match gives Total 0, no Items, no Next", r["Total"] == 0 and r["Items"] == [] and r["Next"] == "")
r = feed(B, gid, q="harbour", person=B.id, limit="all")
check("search and person combine", all(i["UploaderId"] == B.id and "harbour" in i["Name"] for i in r["Items"])
      and r["Total"] == len([n for n in range(230, 250) if n % 4 == 1]), str(r["Total"]))

# ---- 7. feed access ---------------------------------------------------------
print("7. feed access")
check("a user in no group: /gl is 404", outsider.get("/gl?id=%s" % gid)[0] == 404)
check("... and /g is 404", outsider.get("/g?id=%s" % gid)[0] == 404)
check("an unknown group id is 404", B.get("/gl?id=doesnotexist12345")[0] == 404)
for who, c in (("B", B), ("A", A)):
    s, b, h = c.post_json("/gl?id=" + gid, {})
    check("POST /gl is 405 (%s)" % who, s == 405 and h.get("Allow") == "GET", "%s %s" % (s, h.get("Allow")))
s, _, h = B.req("GET", "/gl?id=" + gid)
check("the feed is not cached", "no-store" in h.get("Cache-Control", "") or "no-cache" in h.get("Cache-Control", ""),
      h.get("Cache-Control", ""))
anon = Client("anon")
check("not signed in, /gl does not answer with items", anon.get("/gl?id=%s" % gid)[0] in (301, 302, 303, 307, 401, 404))

# ---- 8. View as… ------------------------------------------------------------
print("8. View as…")
col_path = "/c?id=" + cid
shared_before = B.text("/collections")[1]
check("before: the collection is not in B's 'Shared with me'", cid not in shared_before)
s, mine_before = admin.json("/groups/list")
check("before: the admin is not in G", s == 200 and not any(g["id"] == gid for g in mine_before))
check("a non-admin cannot start it (404)", B.post_json("/viewas/start", {"userId": A.id})[0] == 404)
check("not themselves, not an admin, not nobody (400)",
      admin.post_json("/viewas/start", {"userId": 1})[0] == 400 and admin.post_json("/viewas/start", {})[0] == 400)
check("GET /viewas/start is 405", admin.get("/viewas/start")[0] in (405, 404))

try:
    s, b, _ = admin.post_json("/viewas/start", {"userId": B.id})
    check("1. POST /viewas/start {userId: B} is 200 OK", s == 200 and json.loads(b).get("Result") == "OK", "%s %s" % (s, b[:60]))
    s, mine = admin.json("/groups/list")
    check("2. /groups/list is B's: G, as a member", s == 200 and any(g["id"] == gid and g["role"] == "member" for g in mine),
          str([(g["id"], g["role"]) for g in mine] if s == 200 else s))
    s, page = admin.text("/groups")
    check("   /groups is 200 and shows B's group", s == 200 and gid in page)
    s, page = admin.text("/g?id=" + gid)
    check("   /g?id= is 200", s == 200 and root_attrs(page).get("data-total") == "250", str(s))
    check("   /gl is 200 and is B's view (the 5+5+... ticks are B's)",
          admin.json("/gl?id=%s&limit=1" % gid)[0] == 200)
    a_file = next(i["Id"] for i in walked if i["UploaderId"] == A.id)
    check("3. GET /gd is 403", admin.get("/gd?id=%s&file=%s" % (gid, a_file))[0] == 403)
    s, body, _ = admin.post_form("/gz?id=" + gid, {"files": a_file})
    check("   POST /gz is 403", s == 403, str(s))
    s, body, _ = admin.get("/gz?id=%s&files=%s" % (gid, a_file))
    check("   GET /gz is 403", s == 403, str(s))
    s, body, _ = admin.post_json("/groups/leave", {"id": gid})
    check("   POST /groups/leave is 403", s == 403, "%s %s" % (s, body[:80]))
    check("   ... and the refusal says why", b"read-only" in body, body[:100].decode("utf-8", "replace"))
    check("   POST /collections/create is 403", admin.post_json("/collections/create", {"name": "x", "fileIds": [a_file]})[0] == 403)
    s, _, _ = admin.req("POST", "/auth/token", b"", {"permission": "PERM_VIEW", "Content-Type": "application/json"})
    check("   /auth/token is 403", s == 403, str(s))
    s, _, _ = admin.send_chunk(b"x" * 10, "viewas" + seed.RUN)
    check("   /uploadChunk is 403", s == 403, str(s))
    check("   POST /files/zip and /logout-less changes are refused too",
          admin.post_form("/files/zip", {"ids": a_file})[0] == 403)
    thumb = next(i["Preview"]["Thumb"] for i in walked if i["Preview"]["Thumb"])
    s, body, h = admin.get("/" + thumb.lstrip("./"))
    check("4. /p/thumb of a group file is 200 (an image)", s == 200 and h.get("Content-Type", "").startswith("image/"),
          "%s %s" % (s, h.get("Content-Type")))
    s, _, _ = admin.get(col_path)
    check("5. GET /c of A's collection (viewing as B) is 200", s == 200, str(s))
    check("   the view-as cookie shows B's name", any(c.name == "fgs_viewas" and "p3b" in urllib.parse.unquote(c.value)
                                                 for h in admin.opener.handlers if hasattr(h, "cookiejar") for c in h.cookiejar))
    s, b, _ = admin.post_json("/viewas/end", {})
    check("6. POST /viewas/end is 200 OK", s == 200 and json.loads(b).get("Result") == "OK", "%s %s" % (s, b[:60]))
finally:
    admin.post_json("/viewas/end", {})

s, mine_after = admin.json("/groups/list")
check("   after Exit the admin is out of G again", s == 200 and not any(g["id"] == gid for g in mine_after))
check("   and can download again (not refused)", admin.post_json("/groups/leave", {"id": "nonexistent"})[0] != 403)
shared_after = B.text("/collections")[1]
check("5. after Exit, B's 'Shared with me' is unchanged (no row for A's collection)",
      cid not in shared_after and ("sharedwithmetable" in shared_after) == ("sharedwithmetable" in shared_before))
s, mine = B.json("/groups/list")
check("   B is still in G (the refused leave did nothing)", any(g["id"] == gid and g["role"] == "member" for g in mine))
s, b, _ = admin.req("GET", "/api/logs/get", headers={"apikey": admin.token("PERM_MANAGE_LOGS"), "timestamp": "0"})
logs = json.loads(b).get("logEntries", "") if s == 200 else ""
start_re = r"\[auth\].*\(user #\d+\) started viewing as %s \(user #%d\)" % (re.escape(B.name), B.id)
end_re = r"\[auth\].*\(user #\d+\) stopped viewing as %s \(user #%d\): exit" % (re.escape(B.name), B.id)
check("7. /logs has the start line, category auth", re.search(start_re, logs) is not None)
check("   /logs has the end line ': exit'", re.search(end_re, logs) is not None)
check("   the lines name the admin and the target ('tester (user #n)')",
      re.search(r"tester \(user #\d+\) started viewing as %s" % re.escape(B.name), logs) is not None)
check("   /logs page is 200 for the admin", admin.get("/logs")[0] == 200)

print()
print("ALL PASS" if ok else "SOME FAILED")
sys.exit(0 if ok else 1)
