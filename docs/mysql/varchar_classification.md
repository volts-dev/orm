# 无长度 varchar 在 MySQL 上落成什么（2026-10-24）

用户拍板：无长度 varchar 默认 **VARCHAR(255)**，但逐字段对照 Odoo 19 原定义 + 实际使用情况，需要的改 TEXT 或标长度。
结果在 `varchar_classification.csv`（本机库 public + system 两个 schema 的 3628 列，按 表.列 去重成 1861 个字段）。

| 落成 | 字段数 | 依据 |
|---|---|---|
| VARCHAR(255) | 1634 | Odoo 是 Char / Selection 且数据不超 255；或 Odoo 没有、名字不像长内容 |
| TEXT | 129 | Odoo 是 Text（9，移植时类型标错）；现有数据已超 255（5）；Odoo 是 Char 但用途是长内容（网址、domain、context、说明、留言、OAuth 令牌）且无索引 |
| VARCHAR(128) / VARCHAR(64) | 95 | delivery.carrier 上快递扩展的 FieldChar / FieldSelection——不压的话这张表 115 列 × 1022 字节 ≈ 11.8 万，超 MySQL 单行 65535 |
| VARCHAR(3) / VARCHAR(1024) | 2 | Odoo 原有 size（res.currency.name、ir.attachment.url） |
| 待定 | 1 | catalog.value.name：唯一索引 (param_id, name)，127 万行里 21 行超 255、1 行超 768；MySQL 索引键上限 3072 字节 ≈ 768 字符 |

判定顺序（`classify.py`）：Odoo Text/Html → text；Odoo Char(size) → 原长度；数据 >255 → text（有索引则待定）；
Odoo Char + 用途像长内容（末段是 url/link/domain/context/description/…，排除 code/key/color/ip 与门户 access_token 等短标识）且无索引 → text；
其余 → 255。Odoo 定义来自 `odoo_fields.py`（AST 解析 odoo-19.0 与 saas-19.4 社区源码，展开 _inherit / _inherits）+ 企业版 fields_get 抓取（ai / knowledge / social）。

重跑：`python3 odoo_fields.py odoo19.json <odoo>/odoo/addons <odoo>/addons <saas>/addons`，再按 classify.py 文件头准备 ours.tsv / mf.tsv / our_models.txt（SQL 见会话记录）。

## 落地（2026-10-24）

「TEXT」那一栏没有一律写成 `text()`：vectors 按 TypeName 下发字段类型，`text` 在前端是多行文本框，
一百多个 Odoo 里的单行 Char（网址、令牌、domain……）会全变样。所以分两种：

| 写法 | 字段数 | 含义 |
|---|---|---|
| `varchar() unbounded`（builder `.Unbounded()`，贡献字段 `FieldSpec.Unbounded`） | 116 | 字段类型仍是 char；PG / SQLite 是不带长度的 VARCHAR，MySQL 落 TEXT |
| `text()` | 9 | Odoo 原本就是 Text，移植时标错——改回来，界面跟 Odoo 一样是多行框 |

剔除 5 个：catalog.value.name（用户定改数据、留 255，catalog 8f0f30c）；三个 selection 字段
（documents.document.access_via_link、sys.tenant.archive.reason、sys.tenant.restore.wizard.reason——按名字
误判成长内容，取值最长 12）；mail.message.reaction.content（一个表情，最长 1 字符）。

PG 上这些改动不发 DDL（orm 不改已有列的类型），现有库不受影响；只在 MySQL 新建表时生效。
MySQL 的 TEXT 列默认值写成表达式 `DEFAULT ('…')`（dialect_mysql.go DefaultValueSql）。
