import sys, json, collections, re, glob
D = sys.argv[1]
def rows(p, n):
    for l in open(p):
        x = l.rstrip('\n').split('\t')
        if len(x) >= n: yield x
od = json.load(open(D + '/odoo19.json'))
# 企业版：fields_get 抓下来的（ai / knowledge / social）
for f in glob.glob('/Users/shadow/Softwares/odoo-*-saas19.4/rpc/fields_get.json'):
    for m, fs in json.load(open(f)).items():
        for k, v in fs.items():
            t = {'char': 'Char', 'text': 'Text', 'html': 'Html', 'selection': 'Selection'}.get(v.get('type'), v.get('type'))
            od.setdefault(m, {}).setdefault(k, {'type': t, 'size': v.get('size'), 'translate': bool(v.get('translate')), 'index': None, 'file': 'fields_get:' + f.split('/')[4]})
ALIAS = {
 'pro.tmpl': 'product.template', 'pro.variant': 'product.product', 'pro.attr': 'product.attribute', 'pro.attr.value': 'product.attribute.value',
 'pro.attr.custom.value': 'product.attribute.custom.value', 'pro.cat': 'product.category', 'pro.public.cat': 'product.public.category',
 'pro.tmpl.attr.item': 'product.template.attribute.line', 'pro.tmpl.attr.value': 'product.template.attribute.value',
 'pro.tmpl.attr.exclusion': 'product.template.attribute.exclusion', 'pro.uom': 'uom.uom', 'pro.uom.cat': 'uom.category', 'pro.uom.barcode': 'product.uom',
 'res.user': 'res.users', 'res.group': 'res.groups', 'res.group.privilege': 'res.groups.privilege', 'res.user.settings': 'res.users.settings',
 'sys.user': 'res.users', 'sys.user.apikeys': 'res.users.apikeys', 'sys.user.identitycheck': 'res.users.identitycheck',
 'sys.action': ['ir.actions.act_window', 'ir.actions.server', 'ir.actions.report', 'ir.actions.client', 'ir.actions.act_url', 'ir.actions.actions'],
 'sys.action.window.view': 'ir.actions.act_window.view', 'sys.action.todo': 'ir.actions.todo', 'sys.attachment': 'ir.attachment',
 'sys.config.parameter': 'ir.config_parameter', 'sys.cron': 'ir.cron', 'sys.cron.trigger': 'ir.cron.trigger', 'sys.default': 'ir.default',
 'sys.embedded.actions': 'ir.embedded.actions', 'sys.exports': 'ir.exports', 'sys.exports.line': 'ir.exports.line', 'sys.filters': 'ir.filters',
 'sys.logging': 'ir.logging', 'sys.mail.server': 'ir.mail_server', 'sys.menu': 'ir.ui.menu', 'sys.model': 'ir.model', 'sys.model.access': 'ir.model.access',
 'sys.model.data': 'ir.model.data', 'sys.model.field': 'ir.model.fields', 'sys.module': 'ir.module.module', 'sys.module.category': 'ir.module.category',
 'sys.module.dependency': 'ir.module.module.dependency', 'sys.profile': 'ir.profile', 'sys.rule': 'ir.rule', 'sys.sequence': 'ir.sequence',
 'sys.sequence.date.range': 'ir.sequence.date_range', 'sys.uom': 'uom.uom', 'sys.view': 'ir.ui.view', 'sys.asset': 'ir.asset',
 'sys.model.constraint': 'ir.model.constraint', 'sys.model.relation': 'ir.model.relation', 'sys.model.field.selection': 'ir.model.fields.selection',
 'sys.model.inherit': 'ir.model.inherit', 'sys.ui.view.custom': 'ir.ui.view.custom', 'sys.translation': None,
}
for m in list(ALIAS):
    pass
mf = collections.defaultdict(set)
for x in rows(D + '/mf.tsv', 6): mf[(x[0], x[1])].add(x[2])
models = [l.strip() for l in open(D + '/our_models.txt') if l.strip()]
by_table = collections.defaultdict(set)
for m in models: by_table[m.replace('.', '_')].add(m)
def odoo_of(m):
    a = ALIAS.get(m, m)
    if a is None: return []
    return a if isinstance(a, list) else [a]
LONG = re.compile(r'(^|_)(description|note|notes|comment|comments|help|body|content|message|html|arch|script|domain|context|json|params|payload|traceback|details|reason|prompt|instructions|keywords|url|uri|link|href|endpoint|redirect|token|address_format|contributors|website)$|_domain_')
SHORT = re.compile(r'(^|_)(code|color|ip|mac|key|hash|currency|lang|tz|state|type|uuid|ref|sequence)$|^(access_token|signup_token|token|sms_twilio_auth_token)$')
out = []
seen = {}
for r in rows(D + '/ours.tsv', 10):
    s, t, col, maxlen, nrows, n255, indexed = r[0], r[1], r[2], int(r[3] or 0), int(r[4] or 0), int(r[5] or 0), r[6] == 't'
    key = (t, col)
    ms = sorted(mf.get(key) or by_table.get(t) or [])
    ofs = None; om = ''
    for m in ms:
        for o in odoo_of(m):
            f = od.get(o, {}).get(col)
            if f: ofs = f; om = o; break
        if ofs: break
    otype = ofs['type'] if ofs else ''
    osize = ofs.get('size') if ofs else None
    # 判定
    if otype in ('Text', 'Html'):
        dec, why = 'text', 'Odoo 是 %s（移植时类型不符）' % otype
    elif otype == 'Char' and osize:
        dec, why = 'varchar(%d)' % osize, 'Odoo 是 Char(size=%d)' % osize
    elif maxlen > 255:
        dec, why = ('text+prefix-index' if indexed else 'text'), '现有数据最长 %d 字符%s' % (maxlen, '，且建了索引' if indexed else '')
    elif otype == 'Char' and LONG.search(col) and not SHORT.search(col) and not indexed:
        dec, why = 'text', 'Odoo 是 Char（PG 不限长），按用途判长内容（%s），数据最长 %d' % (LONG.search(col).group(2), maxlen)
    elif otype == 'Char':
        dec, why = 'varchar(255)', 'Odoo 是 Char，数据最长 %d' % maxlen
    elif otype in ('Selection',):
        dec, why = 'varchar(255)', 'Odoo 是 Selection'
    elif otype:
        dec, why = 'varchar(255)', 'Odoo 是 %s，数据最长 %d' % (otype, maxlen)
    elif LONG.search(col) and not SHORT.search(col) and not indexed:
        dec, why = 'text', 'Odoo 没有这个字段；字段名像长内容（%s），数据最长 %d' % (LONG.search(col).group(2), maxlen)
    else:
        dec, why = 'varchar(255)', 'Odoo 没有这个字段，数据最长 %d' % maxlen
    out.append(dict(schema=s, table=t, col=col, model=','.join(ms), odoo_model=om, odoo_type=otype + ('(%s)' % osize if osize else ''),
                    odoo_file=ofs.get('file', '') if ofs else '', maxlen=maxlen, rows=nrows, over255=n255, indexed=indexed, decision=dec, why=why))
json.dump(out, open(D + '/classified.json', 'w'), ensure_ascii=False)
# 去重（public 与 system 同表同列只算一次，取更严的）
rank = lambda d: 0 if d.startswith('varchar(255)') else (2 if d.startswith('text') else 1)
uniq = {}
for o in out:
    k = (o['table'], o['col'])
    if k not in uniq or rank(o['decision']) > rank(uniq[k]['decision']) or o['maxlen'] > uniq[k]['maxlen']:
        if k in uniq and rank(uniq[k]['decision']) > rank(o['decision']): o = dict(o, decision=uniq[k]['decision'], why=uniq[k]['why'])
        uniq[k] = o
U = list(uniq.values())
json.dump(U, open(D + '/classified_uniq.json', 'w'), ensure_ascii=False)
print('columns', len(out), 'unique', len(U))
print('decision', collections.Counter(('text' if u['decision'] == 'text' else ('varchar(255)' if u['decision'] == 'varchar(255)' else 'varchar(n)')) for u in U))
print('odoo match', sum(1 for u in U if u['odoo_type']), '/', len(U))
print('odoo types', collections.Counter(u['odoo_type'].split('(')[0] or '-' for u in U).most_common(12))
print('why', collections.Counter(u['why'].split('，')[0].split('（')[0] for u in U).most_common(12))
