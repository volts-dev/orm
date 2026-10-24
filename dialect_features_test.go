package orm

import (
	"strings"
	"testing"
)

// Features 与老的 SupportReturning 不许各说各的（方言层外的代码只问 Features）。
func TestDialectFeatures_ConsistentWithSupportReturning(t *testing.T) {
	for _, name := range []string{"postgres", "mysql", "sqlite"} {
		d := QueryDialect(name)
		if d == nil {
			t.Fatalf("%s 没注册", name)
		}
		if d.Features().Returning != d.SupportReturning() {
			t.Errorf("%s: Features().Returning=%v，SupportReturning()=%v", name, d.Features().Returning, d.SupportReturning())
		}
	}
	// MySQL 2026-10-24 的能力面：没有原生 RETURNING / 事务内 DDL / 行级安全 / jsonb 运算。
	f := QueryDialect("mysql").Features()
	if f.Returning || f.TransactionalDDL || f.RowSecurity || f.JSONOps || f.UnboundedVarchar {
		t.Errorf("mysql 的能力位打开得太早：%+v", f)
	}
}

// 方言做不到的配置在 New 里就报错，不等到运行时静默失效。
func TestNew_RowScopeEnforceNeedsRowSecurity(t *testing.T) {
	skipUnlessDialect(t, "sqlite", "mysql") // 两者都没有库侧行级安全
	_, err := New(WithDataSource(testDataSource(t, ":memory:")), WithRowScope("app.tenant_id", RowScopeEnforce))
	if err == nil || !strings.Contains(err.Error(), "RowSecurity") {
		t.Fatalf("没有行级安全的方言上开 RowScopeEnforce 应当报错，得到 %v", err)
	}
	o, err := New(WithDataSource(testDataSource(t, ":memory:")), WithRowScope("app.tenant_id", RowScopeAudit))
	if err != nil {
		t.Fatalf("RowScopeAudit 不依赖库侧能力，应当放行：%v", err)
	}
	o.Close()
}

// 公司相关字段、properties 筛选、日期粒度：不支持的方言上报错要点名能力，而不是拼出一条坏 SQL。
func TestFeatureGatedPathsNameTheCapability(t *testing.T) {
	d := QueryDialect("mysql")
	if _, _, err := propertyLeafToSql(d, "t", "props", "color", "=", []any{"red"}); err == nil || !strings.Contains(err.Error(), "JSONOps") {
		t.Errorf("properties 筛选：%v", err)
	}
	if _, _, err := granularityLeafToSql(d, "t", nil, "month", "=", []any{1}, ""); err == nil || !strings.Contains(err.Error(), "DateTrunc") {
		t.Errorf("日期部分筛选：%v", err)
	}
}
