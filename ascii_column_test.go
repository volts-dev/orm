package orm

import (
	stdErrors "errors"
	"path/filepath"
	"strings"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
)

// ascii 列（MySQL 兼容，2026-10-25）：catalog.media.name 是 VARCHAR(2048) 的唯一 URL。MySQL 的索引键上限
// 3072 字节，utf8mb4 按每字符 4 字节算只放得下 768 个字符，这条唯一索引建不出来（1071）。URL 只该是
// ASCII（非 ASCII 字符要百分号编码），列落 ascii 字符集后 2048 字符 = 2048 字节，放得下。
// PG / SQLite 上 ascii 不起作用；三个方言都要建得出唯一索引、重启后 0 DDL。

type AsciiUrlRec struct {
	TModel `table:"name('ascii_url_rec')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(2048) ascii unique required"`
}

func (self AsciiUrlRec) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	self.Builder().VarcharField("alt_url").Size(2048).Ascii()
	return nil
}

// 不标 ascii 的同宽唯一列：MySQL 上要报点名的 ErrIndexUnsupported，而不是裸的 1071。
type WideUniqueRec struct {
	TModel `table:"name('wide_unique_rec')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(2048) unique"`
}

func TestAsciiColumn_WideUniqueIndex(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "ascii.db"))
	long := "https://mm.example.com/docus/" + strings.Repeat("a", 1500) + "/MPLAB%C2%AE%20ICE.pdf"
	for round := 0; round < 2; round++ {
		o, err := New(WithDataSource(ds))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := &ddlRecorder{}
		o.AddHook(rec)
		if _, err := o.SyncModel("", new(AsciiUrlRec)); err != nil {
			t.Fatalf("第 %d 次 SyncModel: %v", round+1, err)
		}
		if round == 1 && len(rec.ddl) > 0 {
			t.Errorf("重启后仍发 DDL：%q", rec.ddl)
		}
		m, _ := o.GetModel("ascii.url.rec")
		if f := m.GetFieldByName("alt_url"); f == nil || !f.Base().ascii {
			t.Fatalf("builder 的 Ascii() 没生效：%v", f)
		}

		s := o.NewSession()
		idx, err := o.dialect.GetIndexes(s.context, s, "ascii_url_rec")
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
		unique := 0
		for _, ix := range idx {
			if ix.Type == UniqueType && len(ix.Cols) == 1 && ix.Cols[0] == "name" {
				unique++
			}
		}
		if unique != 1 {
			t.Fatalf("name 上的唯一索引 %d 条，想要 1：%v", unique, idx)
		}

		if round == 0 {
			if _, err := o.Model("ascii.url.rec").Create(map[string]any{"name": long}); err != nil {
				t.Fatalf("写 %d 字符的 ASCII 地址: %v", len(long), err)
			}
			if _, err := o.Model("ascii.url.rec").Create(map[string]any{"name": long}); err == nil {
				t.Error("同一地址写第二遍应撞唯一索引")
			}
			// 区分大小写，与 PG 一致（ascii_bin）。
			if _, err := o.Model("ascii.url.rec").Create(map[string]any{"name": strings.ToUpper(long)}); err != nil {
				t.Errorf("只差大小写的地址应能写入: %v", err)
			}
		}

		if ds.DbType == "mysql" {
			s := o.NewSession()
			_, cols, err := o.dialect.GetFields(s.context, s, m)
			s.Close()
			if err != nil {
				t.Fatal(err)
			}
			if f := cols["name"]; f == nil || !f.Base().ascii || f.Size() != 2048 {
				t.Errorf("反查 name：%+v，想要 ascii、长度 2048", f)
			}
			// 连接的排序规则是定下的 as_cs，不是驱动默认的 ai_ci（datasource.go mysqlQuery）。
			s = o.NewSession()
			cd, err := s.Query("SELECT @@collation_connection AS c")
			s.Close()
			if err != nil {
				t.Fatal(err)
			}
			if c := cd.FieldByName("c").AsString(); c != "utf8mb4_0900_as_cs" {
				t.Errorf("collation_connection = %q，想要 utf8mb4_0900_as_cs", c)
			}
			_, err = o.Model("ascii.url.rec").Create(map[string]any{"name": "https://x/MPLAB® ICE.pdf"})
			if !stdErrors.Is(err, ormerr.ErrValidation) {
				t.Errorf("非 ASCII 写进 ascii 列应报 ErrValidation，得 %v", err)
			}
		}
		o.Close()
	}

	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	_, err = o.SyncModel("", new(WideUniqueRec))
	if ds.DbType == "mysql" {
		if !stdErrors.Is(err, ormerr.ErrIndexUnsupported) || !strings.Contains(err.Error(), "ascii") {
			t.Errorf("utf8mb4 的 VARCHAR(2048) 唯一索引应报点名 ascii 的 ErrIndexUnsupported，得 %v", err)
		}
	} else if err != nil {
		t.Errorf("%s 上应照建: %v", ds.DbType, err)
	}
}

func TestAscii_RejectsNonCharField(t *testing.T) {
	type BadAscii struct {
		TModel `table:"name('bad_ascii')"`
		Id     int64 `field:"pk autoincr"`
		Qty    int64 `field:"int() ascii"`
	}
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "ba.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(BadAscii)); err == nil || !strings.Contains(err.Error(), "ascii") {
		t.Fatalf("int 字段写 ascii 应报错，得 %v", err)
	}
}
