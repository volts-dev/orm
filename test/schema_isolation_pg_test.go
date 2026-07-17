package test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
)

// 双 schema 租户隔离回归（须真 PG，schema 是 PG 概念，sqlite 无法覆盖）。
//
// 生产对应场景：VectorsSystem 系统租户的 T 表物化在专属 "system" schema（经
// session.SetSchema + SyncModel，见 vectors core/tenant.syncTenantSchema），普通租户
// 共享 public。本测试固化四类曾串库/漏建的路径：
//
//  1. m2m 关联表必须在每个 schema 各建一份（UpdateDb 此前被全局模型注册守卫
//     短路，第二个 schema 的关联表永远漏建）；
//  2. m2m 读（ManyToMany 手拼 SQL）与写（link/unlink_all 裸表名 Exec）必须限定
//     会话 schema，否则全部落到 public——跨租户数据互相污染；
//  3. m2o classic 内嵌子读取（Records() 新会话）必须继承调用方 schema；
//  4. o2m 子读取同上。
type (
	SIGroup struct {
		orm.TModel `table:"name('si_group')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
	}

	SINote struct {
		orm.TModel `table:"name('si_note')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Body       string `field:"varchar()"`
		UserId     int64  `field:"many2one(si_user)"`
	}

	SIUser struct {
		orm.TModel `table:"name('si_user')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"varchar() required"`
		GroupId    int64  `field:"many2one(si_group)"`
		NoteIds    []any  `field:"one2many(si_note,user_id)"`
		GroupIds   []any  `field:"many2many(si_group,si_user_group_rel,user_id,group_id)"`
	}
)

const isoSchema = "iso_sys"

// ensurePG 检查本机 PG 可达并保证 test_orm 库存在；不可达则跳过（CI 无 PG 时不红）。
func ensurePG(t *testing.T) {
	t.Helper()
	admin, err := sql.Open("postgres",
		"host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable")
	if err == nil {
		err = admin.Ping()
	}
	if err != nil {
		t.Skipf("postgres 不可达，跳过 schema 隔离测试: %v", err)
	}
	defer admin.Close()
	var n int
	_ = admin.QueryRow(`SELECT 1 FROM pg_database WHERE datname=$1`, TEST_DB_NAME).Scan(&n)
	if n != 1 {
		if _, err := admin.Exec(`CREATE DATABASE ` + TEST_DB_NAME); err != nil {
			t.Skipf("无法创建测试库 %s: %v", TEST_DB_NAME, err)
		}
	}
}

func TestSchemaIsolationPG(t *testing.T) {
	ensurePG(t)

	o, err := orm.New(orm.WithDataSource(defaultPostgresSource()), orm.WithBigNumberToString(true))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}

	// 幂等清场：连表带 schema 全部重来。
	for _, q := range []string{
		`DROP SCHEMA IF EXISTS ` + isoSchema + ` CASCADE`,
		`DROP TABLE IF EXISTS public.si_user_group_rel`,
		`DROP TABLE IF EXISTS public.si_note`,
		`DROP TABLE IF EXISTS public.si_user`,
		`DROP TABLE IF EXISTS public.si_group`,
		`CREATE SCHEMA ` + isoSchema,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("清场失败 %q: %v", q, err)
		}
	}

	models := []orm.IModel{new(SIGroup), new(SINote), new(SIUser)}

	// public（默认 schema）同步 —— 普通租户路径。
	if _, err := o.SyncModel("test", models...); err != nil {
		t.Fatalf("SyncModel(public): %v", err)
	}
	// iso_sys 同步 —— 镜像 vectors syncTenantSchema：SetSchema + 同一批模型。
	ms := o.NewSession()
	ms.SetSchema(isoSchema)
	if _, err := ms.SyncModel("test", models...); err != nil {
		t.Fatalf("SyncModel(%s): %v", isoSchema, err)
	}
	ms.Close()
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	// ── 1. 关联表必须两个 schema 各有一份 ────────────────────────────────
	for _, sch := range []string{"public", isoSchema} {
		ds, err := o.Query(
			`SELECT count(*) AS n FROM information_schema.tables WHERE table_schema=? AND table_name='si_user_group_rel'`, sch)
		if err != nil {
			t.Fatalf("查 %q 关联表存在性: %v", sch, err)
		}
		if n := ds.FieldByName("n").AsInteger(); n != 1 {
			t.Fatalf("schema %q 缺 m2m 关联表 si_user_group_rel (n=%d)", sch, n)
		}
	}

	// ── 2. 两个 schema 各写一套数据 ─────────────────────────────────────
	type seeded struct {
		userId, groupA, groupB, noteId any
	}
	seed := func(sch, prefix string, nGroups int) seeded {
		ss := o.NewSession()
		defer ss.Close()
		ss.SetSchema(sch)
		if err := ss.Begin(); err != nil {
			t.Fatal(err)
		}
		groupModel, _ := o.GetModel("si_group")
		userModel, _ := o.GetModel("si_user")
		noteModel, _ := o.GetModel("si_note")

		ga, err := groupModel.Tx(ss).Create(map[string]any{"name": prefix + "-GA"})
		if err != nil {
			t.Fatalf("[%s] create group A: %v", sch, err)
		}
		var gb []any
		if nGroups > 1 {
			gb, err = groupModel.Tx(ss).Create(map[string]any{"name": prefix + "-GB"})
			if err != nil {
				t.Fatalf("[%s] create group B: %v", sch, err)
			}
		}
		uids, err := userModel.Tx(ss).Create(map[string]any{"name": prefix + "-U", "group_id": ga[0]})
		if err != nil {
			t.Fatalf("[%s] create user: %v", sch, err)
		}
		nids, err := noteModel.Tx(ss).Create(map[string]any{"body": prefix + "-N1", "user_id": uids[0]})
		if err != nil {
			t.Fatalf("[%s] create note: %v", sch, err)
		}
		if err := ss.Commit(); err != nil {
			t.Fatalf("[%s] commit: %v", sch, err)
		}

		out := seeded{userId: uids[0], groupA: ga[0], noteId: nids[0]}
		if nGroups > 1 {
			out.groupB = gb[0]
		}
		return out
	}
	pub := seed("", "pub", 2)     // public：2 个组
	iso := seed(isoSchema, "iso", 1) // iso_sys：1 个组

	// m2m 写：public 用户挂 2 个组；iso 用户挂 1 个组（写路径走 link/unlink Exec）。
	writeM2M := func(sch string, userId any, groupIds []any) {
		ws := o.NewSession()
		defer ws.Close()
		ws.SetSchema(sch)
		userModel, _ := o.GetModel("si_user")
		if _, err := userModel.Tx(ws).Ids(userId).Write(map[string]any{
			"group_ids": []any{[]any{6, 0, groupIds}},
		}); err != nil {
			t.Fatalf("[%s] write m2m: %v", sch, err)
		}
	}
	writeM2M("", pub.userId, []any{pub.groupA, pub.groupB})
	writeM2M(isoSchema, iso.userId, []any{iso.groupA})

	// ── 3. 关联行必须落在各自 schema 的关联表 ───────────────────────────
	countRel := func(sch string) int {
		ds, err := o.Query(fmt.Sprintf(`SELECT count(*) AS n FROM %s.si_user_group_rel`, sch))
		if err != nil {
			t.Fatalf("count %s rel: %v", sch, err)
		}
		return int(ds.FieldByName("n").AsInteger())
	}
	if n := countRel("public"); n != 2 {
		t.Fatalf("public 关联表期望 2 行(本租户自己的)，实际 %d —— m2m 写串库", n)
	}
	if n := countRel(isoSchema); n != 1 {
		t.Fatalf("%s 关联表期望 1 行，实际 %d —— m2m 写没落进本 schema", isoSchema, n)
	}

	// ── 4. classic 读全关系字段，各 schema 只见自己的数据 ────────────────
	// 断言取 GetByField 的**原始内嵌值**而非 AsMap 输出：AsMap 的标量格式化器行为属
	// dataset 仓职责（在那边有独立回归），本测试只关心 schema 落位是否正确。
	userModel, _ := o.GetModel("si_user")
	classicRead := func(sch string, id any) map[string]any {
		rs := o.NewSession()
		defer rs.Close()
		rs.SetSchema(sch)
		ds, err := userModel.Tx(rs).
			Select("id", "name", "group_id", "group_ids", "note_ids").
			Ids(id).Classic().Read()
		if err != nil {
			t.Fatalf("[%s] classic read: %v", sch, err)
		}
		if ds.Count() != 1 {
			t.Fatalf("[%s] classic read 期望 1 行，实际 %d —— 主表 schema 落位错误", sch, ds.Count())
		}
		rec := ds.Record()
		return map[string]any{
			"group_id":  rec.GetByField("group_id"),
			"group_ids": rec.GetByField("group_ids"),
			"note_ids":  rec.GetByField("note_ids"),
		}
	}

	pm := classicRead("", pub.userId)
	im := classicRead(isoSchema, iso.userId)

	// m2o 内嵌：各自见到各自的组名。两 schema 的组 id 同值(各自序列都从 1 起)，
	// 子读取一旦跨 schema 就会取到对方同 id 组的名字，被这里逮住。
	checkM2O := func(tag string, m map[string]any, wantName string) {
		g, ok := m["group_id"].(map[string]any)
		if !ok {
			t.Fatalf("[%s] group_id 期望内嵌 map，实际 %T=%#v", tag, m["group_id"], m["group_id"])
		}
		if g["name"] != wantName {
			t.Fatalf("[%s] group_id.name 期望 %s，实际 %v —— m2o 子读取跨了 schema", tag, wantName, g["name"])
		}
	}
	checkM2O("public", pm, "pub-GA")
	checkM2O(isoSchema, im, "iso-GA")

	// m2m 内嵌：数量与名字都只属于本 schema。
	pubGroups, _ := pm["group_ids"].([]map[string]any)
	if len(pubGroups) != 2 {
		t.Fatalf("public group_ids 期望 2 组，实际 %#v", pm["group_ids"])
	}
	isoGroups, _ := im["group_ids"].([]map[string]any)
	if len(isoGroups) != 1 {
		t.Fatalf("%s group_ids 期望 1 组，实际 %#v", isoSchema, im["group_ids"])
	}
	if isoGroups[0]["name"] != "iso-GA" {
		t.Fatalf("%s group_ids[0].name 期望 iso-GA，实际 %v —— m2m 读跨了 schema", isoSchema, isoGroups[0]["name"])
	}

	// o2m：iso 用户的 note 列表非空且只含本 schema 的行。
	isoNotes, ok := im["note_ids"].([]any)
	if !ok || len(isoNotes) != 1 {
		t.Fatalf("%s note_ids 期望 1 条，实际 %#v —— o2m 子读取跨了 schema", isoSchema, im["note_ids"])
	}

	// ── 5. 反向兜底：两 schema 的自增序列各自从 1 起，iso 与 pub 的用户 id 大概率
	// 同值——用 iso 的 id 在 public 读，读到的必须是 public 自己的行（名字是 pub-U），
	// 绝不能读到 iso 的行。id 同值反而让串库检测更严格：任何一条子读取跨了 schema，
	// 上面的 m2o/m2m 名字断言就会撞上对方的行而失败。
	rs := o.NewSession()
	ds, err := userModel.Tx(rs).Select("id", "name").Ids(iso.userId).Read()
	rs.Close()
	if err != nil {
		t.Fatalf("反向读: %v", err)
	}
	if ds.Count() == 1 {
		if got := ds.Record().GetByField("name"); got != "pub-U" {
			t.Fatalf("public 用 id=%v 读到 name=%v —— 读到了 iso schema 的行，主表数据串库", iso.userId, got)
		}
	}
}
