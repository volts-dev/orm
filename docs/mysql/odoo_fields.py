# 从 Odoo 源码里抽字段定义：(模型, 字段) -> 类型 / size / translate / index，并展开 _inherit。
import ast, json, os, sys
roots = sys.argv[2:]
own = {}      # model -> {field: info}
parents = {}  # model -> [parent models]
def lit(n):
    try: return ast.literal_eval(n)
    except Exception: return None
for root in roots:
    for dp, dn, fn in os.walk(root):
        if '/tests' in dp or '/static' in dp or '/migrations' in dp: continue
        for f in fn:
            if not f.endswith('.py'): continue
            p = os.path.join(dp, f)
            try: tree = ast.parse(open(p, encoding='utf-8').read())
            except Exception: continue
            for c in ast.walk(tree):
                if not isinstance(c, ast.ClassDef): continue
                name = inh = None; dele = {}; flds = {}
                for st in c.body:
                    if isinstance(st, ast.Assign) and len(st.targets) == 1 and isinstance(st.targets[0], ast.Name):
                        t = st.targets[0].id; v = st.value
                        if t == '_name': name = lit(v)
                        elif t == '_inherit': inh = lit(v)
                        elif t == '_inherits': dele = lit(v) or {}
                        elif isinstance(v, ast.Call):
                            fnm = v.func.attr if isinstance(v.func, ast.Attribute) else (v.func.id if isinstance(v.func, ast.Name) else None)
                            if fnm in ('Char', 'Text', 'Html', 'Selection', 'Binary', 'Json', 'Properties', 'Many2one', 'Integer', 'Float', 'Boolean', 'Date', 'Datetime', 'Monetary', 'Reference', 'Image', 'One2many', 'Many2many', 'Many2oneReference', 'Id'):
                                kw = {k.arg: lit(k.value) for k in v.keywords if k.arg}
                                flds[t] = {'type': fnm, 'size': kw.get('size'), 'translate': bool(kw.get('translate')) if kw.get('translate') is not None else ('translate' in kw),
                                           'index': kw.get('index'), 'related': 'related' in kw, 'store': kw.get('store'), 'compute': 'compute' in kw,
                                           'file': os.path.relpath(p, root)}
                if isinstance(inh, str): inh = [inh]
                inh = [x for x in (inh or []) if isinstance(x, str)] + [k for k in (dele if isinstance(dele, dict) else {}) if isinstance(k, str)]
                model = name if isinstance(name, str) else (inh[0] if inh else None)
                if not model: continue
                own.setdefault(model, {}).update(flds)
                ps = parents.setdefault(model, [])
                for x in inh:
                    if x != model and x not in ps: ps.append(x)
memo = {}
def resolve(m, seen=()):
    if m in memo: return memo[m]
    if m in seen: return {}
    out = {}
    for p in parents.get(m, []):
        out.update(resolve(p, seen + (m,)))
    out.update(own.get(m, {}))
    memo[m] = out
    return out
res = {m: resolve(m) for m in own}
json.dump(res, open(sys.argv[1], 'w'))
print('models', len(res), 'fields', sum(len(v) for v in res.values()))
