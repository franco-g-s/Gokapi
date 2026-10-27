import json, re, html, urllib.request
BASE="http://127.0.0.1:53900"
page=urllib.request.urlopen(BASE+"/setup/start").read().decode("utf-8","replace")
fields={}; checkboxes=set()
for m in re.finditer(r'<(input|select|textarea)\b([^>]*)>', page):
    tag,attrs=m.group(1),m.group(2)
    nm=re.search(r'name="([^"]+)"',attrs)
    if not nm: continue
    name=nm.group(1)
    if tag=="input":
        typ=(re.search(r'type="([^"]+)"',attrs) or [None,"text"])[1]
        val=re.search(r'value="([^"]*)"',attrs)
        if typ=="checkbox": checkboxes.add(name); continue
        fields[name]=html.unescape(val.group(1)) if val else ""
    elif tag=="select":
        blk=re.search(r'<select[^>]*name="%s".*?</select>'%re.escape(name),page,re.S)
        chosen=""
        if blk:
            opts=re.findall(r'<option value="([^"]*)"([^>]*)>',blk.group(0))
            if opts:
                chosen=opts[0][0]
                for v,a in opts:
                    if "selected" in a: chosen=v; break
        fields[name]=chosen
    else: fields[name]=""
fields.update({"public_name":"CollectionTest","url":BASE+"/","ssl_sel":"0","port":"53900",
 "authentication_sel":"0","auth_username":"tester","auth_pw":"testpassword123",
 "auth_pw2":"testpassword123","dbtype_sel":"0","encrypt_sel":"0","storage_sel":"local",
 "url_redirection":"https://example.com/"})
for cb in checkboxes:
    fields.setdefault(cb+".unchecked","false"); fields.pop(cb,None)
req=urllib.request.Request(BASE+"/setup/setupResult",
    data=json.dumps([{"name":k,"value":v} for k,v in fields.items()]).encode(),
    headers={"Content-Type":"application/json"}, method="POST")
print("setup:", urllib.request.urlopen(req).read().decode()[:60])
