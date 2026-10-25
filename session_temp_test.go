package orm

import (
	"errors"
	"path/filepath"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
)

// 事务级临时表三个库一样（P5，2026-10-25，见 session_temp.go）：只在本事务里可见；同名再建一次是清空；
// 提交、回滚之后都不在了——连接池只有一条连接，下一个会话借到的就是同一条，MySQL / SQLite 的临时表挂在
// 连接上，此前（各处手写 ON COMMIT DROP，只有 PG 认）在 MySQL 上整句语法错。
func TestTxTempTable_LivesOnlyInItsTransaction(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "txtemp.db"))
	o, err := New(WithDataSource(ds), WithMaxOpenConns(1), WithMaxIdleConns(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()

	if _, err := o.NewSession().TxTempTable("tt_ids", "(id bigint PRIMARY KEY)"); err == nil {
		t.Fatal("不在事务里应当报错")
	}

	count := func(s *TSession, ref string) int64 {
		t.Helper()
		ds, err := s.Query("SELECT count(*) AS n FROM " + ref)
		if err != nil {
			t.Fatalf("数 %s: %v", ref, err)
		}
		return ds.FieldByName("n").AsInteger()
	}
	for _, end := range []string{"commit", "rollback"} {
		t.Run(end, func(t *testing.T) {
			s := o.NewSession()
			defer s.Close()
			if err := s.Begin(); err != nil {
				t.Fatal(err)
			}
			ref, err := s.TxTempTable("tt_ids", "(id bigint PRIMARY KEY)")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Exec("INSERT INTO "+ref+" (id) VALUES (?), (?)", 1, 2); err != nil {
				t.Fatal(err)
			}
			if n := count(s, ref); n != 2 {
				t.Fatalf("插了 2 行，数出 %d", n)
			}
			if err := s.AnalyzeTxTempTable(ref); err != nil {
				t.Fatalf("AnalyzeTxTempTable: %v", err)
			}
			// 同名再建一次：清空复用。
			if ref2, err := s.TxTempTable("tt_ids", "(id bigint PRIMARY KEY)"); err != nil || ref2 != ref {
				t.Fatalf("再建一次: %q, %v", ref2, err)
			}
			if n := count(s, ref); n != 0 {
				t.Fatalf("再建一次之后应为空，数出 %d", n)
			}
			if _, err := s.Exec("INSERT INTO "+ref+" (id) VALUES (?)", 3); err != nil {
				t.Fatal(err)
			}
			if end == "commit" {
				err = s.Commit()
			} else {
				err = s.Rollback(nil)
			}
			if err != nil {
				t.Fatal(err)
			}

			// 同一条连接上的下一个会话：表已经不在了。
			next := o.NewSession()
			defer next.Close()
			if _, err := next.Query("SELECT count(*) AS n FROM " + ref); !errors.Is(err, ormerr.ErrUndefinedTable) {
				t.Fatalf("%s 之后临时表应当不在（ErrUndefinedTable），得 %v", end, err)
			}
		})
	}
}

func TestImplicitCommitDDL(t *testing.T) {
	for q, want := range map[string]bool{
		"CREATE TABLE x (id int)":                    true,
		"  alter table x add column y int":           true,
		"TRUNCATE x":                                 true,
		"DROP TABLE x":                               true,
		"CREATE INDEX i ON x (y)":                    true,
		"CREATE TEMPORARY TABLE x (id int)":          false,
		"create   temp table x (id int)":             false,
		"DROP TEMPORARY TABLE IF EXISTS x":           false,
		"COMMENT ON COLUMN x.y IS 'z'":               false,
		"INSERT INTO x VALUES (1)":                   false,
		"-- note\nCREATE TABLE x (id int)":           true,
		"SELECT 'CREATE TABLE' AS s FROM x":          false,
		"UPDATE x SET y = 'ALTER TABLE' WHERE 1 = 1": false,
	} {
		if got := implicitCommitDDL(q); got != want {
			t.Errorf("implicitCommitDDL(%q) = %v, want %v", q, got, want)
		}
	}
}

type SyncTxRec struct {
	TModel `table:"name('sync_tx_rec')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

// 结构同步只在 DDL 能随事务回滚的库上包事务：MySQL 上包了也是每条 DDL 隐式提交，还让事务里拿的锁、顺带的回填
// 被悄悄提交（P5）。
func TestSyncModel_TransactionOnlyWithTransactionalDDL(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "synctx.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	rec := &sqlRecorder{}
	o.AddHook(rec)
	if _, err := o.SyncModel("", new(SyncTxRec)); err != nil {
		t.Fatal(err)
	}
	begins, creates := 0, 0
	for _, q := range rec.all {
		switch {
		case q == "BEGIN TRANSACTION":
			begins++
		case implicitCommitDDL(q):
			creates++
		}
	}
	if creates == 0 {
		t.Fatalf("前提：同步应当建了表，记到的语句 %q", rec.all)
	}
	if want := o.Dialect().Features().TransactionalDDL; (begins > 0) != want {
		t.Errorf("TransactionalDDL=%v 时同步开了 %d 次事务", want, begins)
	}
}
