#!/usr/bin/env python3
"""matrix_tests.py <包目录> —— 输出方言矩阵要跑的用例名正则（go test -run 用）。

方言矩阵（MySQL 兼容 P0，2026-10-24）：同一套用例在 SQLite / PostgreSQL / MySQL 上各跑一遍，数据源由
testdb_test.go 的 testDataSource 按 ORM_TEST_DB 给。这里**排除**直连本机 PG（localhost:5432）的老用例：
正文里出现 postgres / 5432 / lib/pq 的函数，以及调用了它们的（往外传三层）。testdb_test.go 自己不算
（它的 postgres 分支连的是 docker 里的测试库）。
"""
import glob, os, re, sys

d = sys.argv[1] if len(sys.argv) > 1 else '.'
funcs = {}
for f in glob.glob(os.path.join(d, '*_test.go')):
    src = open(f, encoding='utf-8').read()
    for m in re.finditer(r'^func (?:\([^)]*\) )?(\w+)\(.*?^}', src, re.S | re.M):
        funcs[m.group(1)] = (os.path.basename(f), m.group(0))
pg = {n for n, (f, b) in funcs.items() if f != 'testdb_test.go' and re.search(r'postgres|POSTGRES|5432|lib/pq', b)}
for _ in range(3):
    for n, (_, b) in funcs.items():
        if n not in pg and any(re.search(r'\b%s\(' % re.escape(h), b) for h in pg):
            pg.add(n)
tests = sorted(n for n in funcs if n.startswith('Test') and n not in pg)
if '-n' in sys.argv:
    print(len(tests), 'in matrix;', sum(1 for n in funcs if n.startswith('Test') and n in pg), 'excluded (local PG)')
else:
    print('^(' + '|'.join(tests) + ')$')
