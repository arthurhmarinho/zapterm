import json, sys
src, out = sys.argv[1], sys.argv[2]
def ann(l):
    a = json.load(open(f"{src}/ann-{l}.json"))["annotations"]["annotations"]
    return {k: v for k, v in a.items()}
pt, en = ann("pt"), ann("en")
def look(table, e):
    return table.get(e) or table.get(e.replace("️", ""))
rows, seen = [], set()
for line in open(f"{src}/emoji-test.txt", encoding="utf-8"):
    if line.startswith("#") or ";" not in line:
        continue
    cps, rest = line.split(";", 1)
    if "fully-qualified" not in rest:
        continue
    name = rest.split("#", 1)[1]
    if "skin tone" in name:
        continue
    e = "".join(chr(int(c, 16)) for c in cps.split())
    p, n = look(pt, e), look(en, e)
    if not p and not n:
        continue
    def words(a):
        if not a: return []
        return [a.get("tts", [""])[0]] + [w for w in a.get("default", []) if w != a.get("tts", [""])[0]]
    w = words(p) + words(n)
    clean = []
    for x in w:
        x = x.strip().lower().replace("\t", " ")
        if x and x not in clean:
            clean.append(x)
    if e in seen: continue
    seen.add(e)
    rows.append(e + "\t" + "|".join(clean))
open(out, "w", encoding="utf-8").write("\n".join(rows) + "\n")
print(len(rows), "emojis")
