#!/usr/bin/env python3
"""matrix_check.py <go test 输出> <基线文件> [--update] —— 方言矩阵的结果对基线。

基线（test/baseline/<方言>.txt）是该方言上**已知失败**的顶层用例名，一行一个。退出码：
  0  失败集合 ⊆ 基线（可能有用例修好了——会列出来，提示把它从基线删掉）
  1  出现了基线之外的新失败
--update 用本次的失败集合覆盖基线（修好一批之后用）。
"""
import re, sys

log, base = sys.argv[1], sys.argv[2]
out = open(log, encoding='utf-8', errors='replace').read()
failed = set(re.findall(r'^--- FAIL: (\S+)', out, re.M))
passed = set(re.findall(r'^--- PASS: (\S+)', out, re.M))
if re.search(r'^(panic:|fatal error:)', out, re.M):
    print('！整包崩了（panic / fatal），结果不完整：先看日志', log)
    sys.exit(1)
try:
    known = {l.strip() for l in open(base, encoding='utf-8') if l.strip() and not l.startswith('#')}
except FileNotFoundError:
    known = set()
if '--update' in sys.argv:
    hdr = [l for l in open(base, encoding='utf-8') if l.startswith('#')] if known or open(base).read() else []
    with open(base, 'w', encoding='utf-8') as f:
        f.writelines(hdr)
        f.write('\n'.join(sorted(failed)) + ('\n' if failed else ''))
    print('基线已更新：%d 个已知失败' % len(failed))
    sys.exit(0)
new = sorted(failed - known)
fixed = sorted(known & passed)
print('失败 %d（基线 %d）；新失败 %d；已修好 %d' % (len(failed), len(known), len(new), len(fixed)))
for n in new: print('  新失败：', n)
for n in fixed: print('  已修好（从基线删掉）：', n)
sys.exit(1 if new else 0)
