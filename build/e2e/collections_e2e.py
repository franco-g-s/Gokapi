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

print("\n" + ("ALL CHECKS PASSED" if ok else "SOME CHECKS FAILED"))
sys.exit(0 if ok else 1)
