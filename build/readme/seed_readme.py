#!/usr/bin/env python3
"""Seeds a throwaway instance with the made-up people and photos the README
screenshots show.

Against 127.0.0.1:53900, set up by build/e2e/setup_local.py, with the
container started with -e GOKAPI_E2E_SEED=1 (for the profile photos).
ASSETS is a folder of JPEGs and one MP4 from make_photos.py (converted with
vips; see the README's "Screenshots" section).

    python3 build/readme/seed_readme.py ASSETS

Prints one JSON line: the people's account names and password, the group id
and the collection id.
"""
import base64, importlib.util, json, os, sys

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("seed_part3", os.path.join(HERE, "..", "e2e", "seed_part3.py"))
seed = importlib.util.module_from_spec(spec)
spec.loader.exec_module(seed)
Client, log, retry = seed.Client, seed.log, seed.retry

PEOPLE = [  # account, shown name, avatar or None for initials
    ("anna@example.com", "Anna Muster", "avatar-0.jpg"),
    ("ben@example.com", "Ben Keller", "avatar-1.jpg"),
    ("carla@example.com", "Carla Rossi", "avatar-2.jpg"),
    ("elif@example.com", "Elif Demir", None),
]
GROUP = "Engadin weekend"
COLLECTION = "Sunsets"
PDF = (b"%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj 2 0 obj<</Type/Pages/Kids[]/Count 0>>endobj\n"
       b"trailer<</Root 1 0 R>>\n%%EOF\n")


def person(admin, account, shown):
    s, b, _ = admin.req("POST", "/api/user/create", b"",
                        {"apikey": admin.token("PERM_MANAGE_USERS"), "username": account})
    if s != 200:
        raise RuntimeError("user/create %s -> %s %s" % (account, s, b[:200]))
    uid = json.loads(b)["id"]
    s, b, _ = admin.req("POST", "/api/user/resetPassword", b"",
                        {"apikey": admin.token("PERM_MANAGE_USERS"), "userid": str(uid),
                         "generateNewPassword": "true"})
    c = Client(account)
    c.id = uid
    c.login(account, json.loads(b)["password"])
    _, page = c.text("/changePassword")      # first login forces a new password
    c.post_form("/changePassword", {"newpw": seed.NEW_PW, "csrf-token": c.csrf(page)})
    s, b, _ = c.post_json("/settings/save", {"displayName": shown})
    if s != 200:
        raise RuntimeError("settings/save %s -> %s %s" % (shown, s, b[:200]))
    return c


def upload(client, path, group=""):
    with open(path, "rb") as f:
        content = f.read()
    name = os.path.basename(path)

    def attempt():
        s, r = client.upload(name, content, group)
        return (r.get("FileInfo") or {}).get("Id", "") if s == 200 else ""
    fid = retry(attempt, tries=6, delay=1)
    if not fid:
        raise RuntimeError("upload of %s failed" % name)
    return fid


def main():
    assets = sys.argv[1]
    photos = sorted(f for f in os.listdir(assets) if f.endswith(".jpg") and not f.startswith("avatar-"))
    admin = Client("admin")
    if admin.login(seed.ADMIN, seed.ADMIN_PW) not in (302, 303, 307):
        raise RuntimeError("admin login failed")
    admin.post_json("/settings/save", {"displayName": "Sam"})
    log("people")
    clients = []
    for account, shown, avatar in PEOPLE:
        c = person(admin, account, shown)
        if avatar:
            with open(os.path.join(assets, avatar), "rb") as f:
                jpeg = base64.b64encode(f.read()).decode()
            s, b, _ = admin.post_json("/seed/photo", {"userId": c.id, "jpegBase64": jpeg})
            if s != 200:
                raise RuntimeError("seed/photo -> %s %s (is GOKAPI_E2E_SEED=1 set?)" % (s, b[:200]))
        clients.append(c)
    anna = clients[0]
    log("group")
    s, b, _ = anna.post_json("/groups/create", {"name": GROUP, "days": 30})
    created = json.loads(b)
    if s != 200 or created.get("Result") != "OK":
        raise RuntimeError("groups/create -> %s %s" % (s, b[:200]))
    gid = created["Id"]
    for c in clients[1:]:
        s, b, _ = c.post_json("/groups/join", {"code": created["Code"]})
        if s != 200:
            raise RuntimeError("groups/join -> %s %s" % (s, b[:200]))
    log("uploads")
    group_photos, own = photos[:18], photos[18:]
    ids = {}
    for i, name in enumerate(group_photos):
        ids[name] = upload(clients[i % len(clients)], os.path.join(assets, name), gid)
    upload(clients[1], os.path.join(assets, "Boat across the lake.mp4"), gid)
    mine = [upload(anna, os.path.join(assets, name)) for name in own]
    pdf = os.path.join(assets, "Hut booking.pdf")
    with open(pdf, "wb") as f:
        f.write(PDF)
    upload(anna, pdf)
    log("collection")
    s, b, _ = anna.post_json("/collections/create", {"name": COLLECTION, "fileIds": mine})
    col = json.loads(b)
    if s != 200 or col.get("Result") != "OK":
        raise RuntimeError("collections/create -> %s %s" % (s, b[:200]))
    print(json.dumps({"people": [p[0] for p in PEOPLE], "password": seed.NEW_PW,
                      "group": gid, "collection": col["Id"]}), flush=True)


if __name__ == "__main__":
    main()
