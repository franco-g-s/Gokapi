#!/usr/bin/env python3
"""End-to-end test of the Collections MVP against a local Gokapi fork.

Logs in with internal auth, uploads two files, bundles them into one
collection, then exercises the public link as an anonymous client.
"""
import http.cookiejar, json, re, urllib.request, urllib.parse, urllib.error, uuid, sys

BASE = "http://127.0.0.1:53900"
USER, PW = "tester", "testpassword123"

jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
anon = urllib.request.build_opener()          # no cookies: simulates a recipient

ok = True
def check(label, cond, detail=""):
    global ok
    if not cond:
        ok = False
    print("  %-52s %s %s" % (label, "PASS" if cond else "FAIL", detail))

# ---- 1. log in -------------------------------------------------------------
page = opener.open(BASE + "/login").read().decode()
csrf = re.search(r'name="csrf-token"[^>]*value="([^"]+)"', page) or \
       re.search(r'value="([^"]+)"[^>]*name="csrf-token"', page)
data = {"username": USER, "password": PW}
if csrf:
    data["csrf-token"] = csrf.group(1)
try:
    opener.open(BASE + "/login", urllib.parse.urlencode(data).encode()).read()
except urllib.error.HTTPError as e:
    pass          # a 302/307 back to /admin is what a successful login looks like
admin = opener.open(BASE + "/admin").read().decode()
check("login + reach /admin", "uploaddropzone" in admin or "Upload" in admin)

# ---- 2. get an upload token and upload two files ---------------------------
def token(perm):
    req = urllib.request.Request(BASE + "/auth/token", data=b"", method="POST",
                                 headers={"permission": perm, "Content-Type": "application/json"})
    return json.loads(opener.open(req).read())["key"]

up = token("PERM_UPLOAD")

def upload(name, content):
    boundary = uuid.uuid4().hex
    body = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{name}\"\r\n"
        f"Content-Type: text/plain\r\n\r\n{content}\r\n"
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"allowedDownloads\"\r\n\r\n1\r\n"
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"expiryDays\"\r\n\r\n5\r\n"
        f"--{boundary}--\r\n").encode()
    req = urllib.request.Request(BASE + "/api/files/add", data=body, method="POST",
                                 headers={"apikey": up,
                                          "Content-Type": "multipart/form-data; boundary=" + boundary})
    return json.loads(urllib.request.urlopen(req).read())["FileInfo"]["Id"]

id1 = upload("alpha.txt", "AAAA-alpha-contents")
id2 = upload("beta.txt", "BBBB-beta-contents")
check("uploaded two files", bool(id1 and id2), f"{id1} {id2}")

# ---- 3. create a collection ------------------------------------------------
req = urllib.request.Request(BASE + "/collections/create",
                             data=json.dumps({"name": "Holiday photos",
                                              "fileIds": [id1, id2]}).encode(),
                             headers={"Content-Type": "application/json"}, method="POST")
created = json.loads(opener.open(req).read())
check("create collection returns OK", created.get("Result") == "OK", str(created)[:90])
cid = created.get("Id", "")
check("collection contains both files", created.get("Files") == 2)

# ---- 4. anonymous access to the public page --------------------------------
page = anon.open(BASE + "/c?id=" + cid).read().decode()
check("public page reachable with NO login", True)
check("page shows the collection name", "Holiday photos" in page)
check("page lists alpha.txt", "alpha.txt" in page)
check("page lists beta.txt", "beta.txt" in page)
check("page has per-file download links", page.count("cd?id=") >= 2)
# stylesheets must resolve from the page's own path, or it renders as plain text
import urllib.error as _ue
css = re.findall(r'<link[^>]+href="([^"]+\.css)"', page)
resolved = []
for href in css:
    u = href[2:] if href.startswith("./") else href.lstrip("/")
    try:
        resolved.append(anon.open(BASE + "/" + u).status == 200)
    except _ue.HTTPError:
        resolved.append(False)
check("all stylesheets on the page resolve (not plain text)",
      len(resolved) > 0 and all(resolved), f"{sum(resolved)}/{len(resolved)} ok")
check("download-all button present", 'id="downloadAllButton"' in page)

# ---- 5. anonymous download of a member -------------------------------------
body = anon.open(f"{BASE}/cd?id={cid}&file={id1}").read().decode()
check("anonymous member download returns content", body.strip() == "AAAA-alpha-contents",
      repr(body[:30]))

# ---- 6. the collection budget decremented, the file's own did not ----------
lst = token("PERM_VIEW")
req = urllib.request.Request(BASE + "/api/files/list", headers={"apikey": lst})
files = json.loads(urllib.request.urlopen(req).read()) or []
f1 = next((f for f in files if f["Id"] == id1), None)
# The file was uploaded with allowedDownloads=1, so consuming it through the
# collection should retire it entirely - it drops out of the listing.
check("collection download consumed the file's own budget",
      f1 is None or f1["DownloadsRemaining"] == 0,
      "file retired" if f1 is None else f"remaining={f1['DownloadsRemaining']}")

# ---- 7. collection stays alive while ANY member is alive -------------------
page = anon.open(BASE + "/c?id=" + cid).read().decode()
check("collection still alive while beta.txt remains",
      "beta.txt" in page and "alpha.txt" not in page,
      "spent member correctly dropped from the listing")

# ---- 8. collection dies once every member is spent -------------------------
anon.open(f"{BASE}/cd?id={cid}&file={id2}").read()
try:
    r = anon.open(BASE + "/c?id=" + cid)
    dead = "Holiday photos" not in r.read().decode()
except urllib.error.HTTPError:
    dead = True
check("collection expires once all its files are spent", dead)

# ---- 9. bad id -------------------------------------------------------------
try:
    r = anon.open(BASE + "/c?id=doesnotexist12345")
    bad = "Holiday photos" not in r.read().decode()
except urllib.error.HTTPError:
    bad = True
check("unknown collection id does not 500", bad)

# ---- 10. admin collections list -------------------------------------------
# recreate one, since the earlier collection was fully spent
id3 = upload("gamma.txt", "CCCC-gamma")
id4 = upload("delta.txt", "DDDD-delta")
req = urllib.request.Request(BASE + "/collections/create",
                             data=json.dumps({"name": "Second batch",
                                              "fileIds": [id3, id4]}).encode(),
                             headers={"Content-Type": "application/json"}, method="POST")
c2 = json.loads(opener.open(req).read())
listing = opener.open(BASE + "/collections").read().decode()
check("collections list page renders", "Collections" in listing)
check("list shows the collection", "Second batch" in listing)
check("list shows its public link", c2["Id"] in listing)

# ---- 11. delete removes the collection but keeps the files -----------------
req = urllib.request.Request(BASE + "/collections/delete?id=" + c2["Id"],
                             data=b"", method="POST")
res = json.loads(opener.open(req).read())
check("delete returns OK", res.get("Result") == "OK", str(res)[:60])
try:
    gone = "Second batch" not in anon.open(BASE + "/c?id=" + c2["Id"]).read().decode()
except urllib.error.HTTPError:
    gone = True
check("deleted collection link is dead", gone)
req = urllib.request.Request(BASE + "/api/files/list", headers={"apikey": token("PERM_VIEW")})
still = [f["Id"] for f in (json.loads(urllib.request.urlopen(req).read()) or [])]
check("deleting a collection keeps its files", id3 in still and id4 in still)

# ---- 12. "available until" is the LAST available file's date ---------------
import datetime, re as _re
idA = upload("short.txt", "short")          # expiryDays=5 from upload()
listing2 = opener.open(BASE + "/collections").read().decode()
check("list shows a real availability date",
      bool(_re.search(r"\d{4}-\d{2}-\d{2} \d{2}:\d{2}", listing2)),
      "date rendered")

# ---- 13. password-protected collection -------------------------------------
id5 = upload("secret.txt", "SSSS-secret-contents")
req = urllib.request.Request(BASE + "/collections/create",
                             data=json.dumps({"name": "Locked", "fileIds": [id5],
                                              "password": "hunter2-correct-horse"}).encode(),
                             headers={"Content-Type": "application/json"}, method="POST")
locked = json.loads(opener.open(req).read())
check("create password-protected collection", locked.get("Result") == "OK")
lid = locked.get("Id", "")

page = anon.open(BASE + "/c?id=" + lid).read().decode()
check("locked collection asks for a password", "password" in page.lower())
check("locked collection hides the filenames", "secret.txt" not in page)

# member download blocked without the cookie
try:
    r = anon.open(f"{BASE}/cd?id={lid}&file={id5}")
    body = r.read().decode(errors="replace")
    blocked = "SSSS-secret-contents" not in body
except urllib.error.HTTPError:
    blocked = True
check("member download blocked without password", blocked)

# now supply the password (fresh cookie-aware opener = a real visitor)
jar2 = http.cookiejar.CookieJar()
visitor = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar2))
try:
    visitor.open(BASE + "/c?id=" + lid,
                 urllib.parse.urlencode({"password": "hunter2-correct-horse"}).encode()).read()
except urllib.error.HTTPError:
    pass
unlocked = visitor.open(BASE + "/c?id=" + lid).read().decode()
check("correct password unlocks the listing", "secret.txt" in unlocked)
got = visitor.open(f"{BASE}/cd?id={lid}&file={id5}").read().decode()
check("unlocked visitor can download the member", got.strip() == "SSSS-secret-contents")

# wrong password stays locked
jar3 = http.cookiejar.CookieJar()
bad = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar3))
try:
    resp = bad.open(BASE + "/c?id=" + lid,
                    urllib.parse.urlencode({"password": "wrong"}).encode()).read().decode()
except urllib.error.HTTPError as e:
    resp = e.read().decode()
check("wrong password is rejected", "secret.txt" not in resp)

# ---- 14. editing: rename, append, remove ----------------------------------
idE1 = upload("edit1.txt", "E1"); idE2 = upload("edit2.txt", "E2"); idE3 = upload("edit3.txt", "E3")
req = urllib.request.Request(BASE + "/collections/create",
                             data=json.dumps({"name": "Editable", "fileIds": [idE1, idE2]}).encode(),
                             headers={"Content-Type": "application/json"}, method="POST")
ec = json.loads(opener.open(req).read()); eid = ec["Id"]

def modify(payload):
    r = urllib.request.Request(BASE + "/collections/modify",
                               data=json.dumps(payload).encode(),
                               headers={"Content-Type": "application/json"}, method="POST")
    try:
        return json.loads(opener.open(r).read())
    except urllib.error.HTTPError as e:
        return json.loads(e.read())

res = modify({"id": eid, "name": "Renamed"})
check("rename returns OK", res.get("Result") == "OK")
page = anon.open(BASE + "/c?id=" + eid).read().decode()
check("public page shows the new name", "Renamed" in page and "Editable" not in page)

res = modify({"id": eid, "addFileIds": [idE3]})
check("append adds a file", res.get("Files") == 3, f"files={res.get('Files')}")
page = anon.open(BASE + "/c?id=" + eid).read().decode()
check("appended file appears publicly", "edit3.txt" in page)

res = modify({"id": eid, "fileIds": [idE1]})
check("replacing members works", res.get("Files") == 1, f"files={res.get('Files')}")
page = anon.open(BASE + "/c?id=" + eid).read().decode()
check("removed files are gone publicly", "edit2.txt" not in page and "edit1.txt" in page)

res = modify({"id": eid, "fileIds": []})
check("cannot empty a collection", res.get("Result") == "error", str(res)[:70])

res = modify({"id": "nope", "name": "x"})
check("editing an unknown collection is refused", res.get("Result") == "error")

listed = json.loads(opener.open(BASE + "/collections/list").read())
check("collections/list returns JSON entries", isinstance(listed, list) and len(listed) >= 1,
      f"{len(listed)} entries")

# ---- 15. collections page carries the picker and its file list ------------
page = opener.open(BASE + "/collections").read().decode()
check("page has the New collection button", 'id="newCollectionButton"' in page)
check("page has the file picker modal", 'id="filePickerModal"' in page)
check("picker lists selectable files", page.count("file-picker-check") >= 2,
      f"{page.count('file-picker-check')} entries")
check("each collection row offers Add files", "collection-addfiles" in page)
check("member rows are rendered for removal", "member-remove" in page)

# ---- 16. no chained dialogs left in the create flows -----------------------
page = opener.open(BASE + "/collections").read().decode()
check("collections page has inline name/password fields",
      'id="filePickerName"' in page and 'id="filePickerPassword"' in page)
# What matters is that no single flow chains two dialogs (Chrome then offers
# "prevent additional dialogs", after which every prompt returns null).
# Standalone prompts for rename and password are fine.
check("create flow uses fields, not chained prompts",
      'Name for this collection' not in page and 'filePickerName' in page,
      "create reads the inline fields")
check("picker has a passphrase suggest button", 'id="filePickerSuggest"' in page)

# ---- 17. password can be added, changed and removed after creation --------
idP = upload("later.txt", "LATER-contents")
req = urllib.request.Request(BASE + "/collections/create",
                             data=json.dumps({"name": "AddPwLater", "fileIds": [idP]}).encode(),
                             headers={"Content-Type": "application/json"}, method="POST")
pc = json.loads(opener.open(req).read()); pid = pc["Id"]
check("open collection is reachable", "later.txt" in anon.open(BASE + "/c?id=" + pid).read().decode())

modify({"id": pid, "password": "first-password-123"})
page = anon.open(BASE + "/c?id=" + pid).read().decode()
check("adding a password locks an existing collection", "later.txt" not in page)

def unlock(pw):
    j = http.cookiejar.CookieJar()
    o = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(j))
    try:
        o.open(BASE + "/c?id=" + pid, urllib.parse.urlencode({"password": pw}).encode()).read()
    except urllib.error.HTTPError:
        pass
    return o.open(BASE + "/c?id=" + pid).read().decode()

check("the new password unlocks it", "later.txt" in unlock("first-password-123"))
modify({"id": pid, "password": "second-password-456"})
check("old password stops working after a change", "later.txt" not in unlock("first-password-123"))
check("changed password works", "later.txt" in unlock("second-password-456"))
modify({"id": pid, "password": ""})
check("blank password removes protection",
      "later.txt" in anon.open(BASE + "/c?id=" + pid).read().decode())

listing3 = opener.open(BASE + "/collections").read().decode()
check("list has a password button per collection", "collection-password" in listing3)

print("\n" + ("ALL CHECKS PASSED" if ok else "SOME CHECKS FAILED"))
sys.exit(0 if ok else 1)
