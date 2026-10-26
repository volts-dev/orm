package orm

import (
	"context"
	"testing"
)

// MySQL 上系统租户的表在同实例的另一个库里：运行时补字段（列、many2many 关联表）也要补到那个库——只补声明过的库，
// 同实例里别的库的同名表不碰（2026-10-26 整库搬家演练：website_sale 给 pro.tmpl 贡献的关联表在 system 库里没建）。

type xsOwner struct {
	TModel `table:"name('xs_owner')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

type xsTarget struct {
	TModel `table:"name('xs_target')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

func TestAddField_MaterializesIntoExtraSchemas(t *testing.T) {
	skipUnlessDialect(t, "mysql")
	ds := testDataSource(t, "")
	extra, stranger := ds.DbName+"_sys", ds.DbName+"_other"
	admin, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []string{extra, stranger} {
		for _, q := range []string{
			`CREATE DATABASE "` + db + `" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs`,
			`CREATE TABLE "` + db + `".xs_owner (id bigint PRIMARY KEY AUTO_INCREMENT, name varchar(32))`,
		} {
			if _, err := admin.Exec(q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		db := db
		t.Cleanup(func() { admin.Exec(`DROP DATABASE IF EXISTS "` + db + `"`) })
	}
	t.Cleanup(func() { admin.Close() })

	o, err := New(WithDataSource(ds), WithExtraSchemas(extra))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	names, err := o.SyncModel("test", new(xsOwner), new(xsTarget))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	owner, target := names[0], names[1]
	if _, err := o.AddField(owner, "note", "varchar(64) title('Note')"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.AddField(owner, "target_ids", "many2many("+target+",xs_owner_target_rel,owner_id,target_id) title('Targets')"); err != nil {
		t.Fatal(err)
	}
	count := func(q string, args ...any) int64 {
		d, err := o.Query(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		return d.FieldByName("n").AsInteger()
	}
	for _, c := range []struct {
		db   string
		want int64
	}{{ds.DbName, 1}, {extra, 1}, {stranger, 0}} {
		if n := count(`SELECT COUNT(*) AS n FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'xs_owner' AND COLUMN_NAME = 'note'`, c.db); n != c.want {
			t.Errorf("库 %s 的 xs_owner.note：%d 列，想要 %d", c.db, n, c.want)
		}
		if n := count(`SELECT COUNT(*) AS n FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'xs_owner_target_rel'`, c.db); n != c.want {
			t.Errorf("库 %s 的关联表 xs_owner_target_rel：%d 张，想要 %d", c.db, n, c.want)
		}
	}
}
