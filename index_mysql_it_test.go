package orm

import (
	"context"
	stdErrors "errors"
	"os"
	"testing"

	"github.com/volts-dev/dataset"
	ormerr "github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

// MySQL 上的部分索引 / 表达式索引端到端集成。
//
// 沿用仓库既有约定（test/orm_test.go）：默认 Skip，设了 MYSQL_TEST_HOST 才连库，
// 凭据从环境变量取。8.0.13+ 走 functional key parts（唯一部分索引用 CASE WHEN 模拟、
// 表达式走 ((expr))）；更低版本 orm 会拒绝，本用例只在能连上的库上跑。
//
//	MYSQL_TEST_HOST=127.0.0.1 MYSQL_TEST_PORT=3306 \
//	MYSQL_TEST_USER=root MYSQL_TEST_PASS=xxx go test -run MySQLIndex_ .
func newMysqlIndexOrm(t *testing.T) *TOrm {
	t.Helper()
	host := os.Getenv("MYSQL_TEST_HOST")
	if host == "" {
		t.Skip("MYSQL_TEST_HOST not set; skipping MySQL live index tests")
	}
	port := os.Getenv("MYSQL_TEST_PORT")
	if port == "" {
		port = "3306"
	}
	dbName := os.Getenv("MYSQL_TEST_DB")
	if dbName == "" {
		dbName = "test_orm"
	}
	ds := &TDataSource{DbType: "mysql", Host: host, Port: port,
		UserName: os.Getenv("MYSQL_TEST_USER"), Password: os.Getenv("MYSQL_TEST_PASS"), DbName: dbName}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS myi_job`); err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}
	t.Cleanup(func() { o.Exec(`DROP TABLE IF EXISTS myi_job`) })
	return o
}

type myiJob struct {
	TModel   `table:"name('myi_job')"`
	Id       int64  `field:"pk autoincr"`
	TenantId int64  `field:"int()"`
	JobKey   string `field:"varchar(64)"`
	State    string `field:"varchar(16)"`
	Name     string `field:"varchar(64)"`
}

func (self *myiJob) OnBuildFields() error {
	b := self.Builder()
	b.SetPartialUniqueIndex("state = 'pending'", "tenant_id", "job_key")
	b.SetUniqueExprIndex("lower(name)")
	b.SetPartialIndex("state = 'done'", "name")
	return b.Err()
}

func TestMySQLIndex_PartialUniqueEmulationWorks(t *testing.T) {
	o := newMysqlIndexOrm(t)
	if !o.dialect.(*mysql).supportsFunctionalIndex() {
		t.Skip("this MySQL lacks functional key parts (<8.0.13); orm rejects such indexes by design")
	}
	if _, err := o.SyncModel("", new(myiJob)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	c := func(tenant int64, key, state, name string) error {
		_, err := o.Model("myi.job").Create(map[string]any{
			"tenant_id": tenant, "job_key": key, "state": state, "name": name})
		return err
	}
	if err := c(1, "k1", "pending", "Alice"); err != nil {
		t.Fatal(err)
	}
	// 谓词外：CASE WHEN 把键折成 NULL，NULL 不参与唯一冲突 → 不冲突
	if err := c(1, "k1", "done", "Bob"); err != nil {
		t.Fatalf("谓词外的行不该受部分唯一约束：%v", err)
	}
	if err := c(1, "k1", "done", "Carol"); err != nil {
		t.Fatalf("谓词外的行彼此也不该冲突（都是 NULL 键）：%v", err)
	}
	// 谓词内：CASE WHEN 保留真实键 → 冲突
	if err := c(1, "k1", "pending", "Dave"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("pending 内重复应 ErrDuplicate，得到 %v", err)
	}
	// 表达式唯一：lower(name)
	if err := c(2, "k2", "pending", "alice"); err == nil || !stdErrors.Is(err, ormerr.ErrDuplicate) {
		t.Fatalf("lower(name) 唯一应拦下 alice/Alice，得到 %v", err)
	}
}

func TestMySQLIndex_IntrospectionIdempotent(t *testing.T) {
	o := newMysqlIndexOrm(t)
	if !o.dialect.(*mysql).supportsFunctionalIndex() {
		t.Skip("this MySQL lacks functional key parts (<8.0.13)")
	}
	if _, err := o.SyncModel("", new(myiJob)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	model, _ := o.GetModel("myi.job")
	declared := model.GetIndexes()
	check := func(label string) {
		got, err := o.dialect.GetIndexes(context.Background(), nil, "myi_job")
		if err != nil {
			t.Fatalf("%s GetIndexes: %v", label, err)
		}
		for _, idx := range declared {
			db := got[idx.GetName("myi_job")]
			if db == nil {
				t.Fatalf("%s: 库里缺 %s；库里有 %v", label, idx.GetName("myi_job"), keysOf(got))
			}
			if !db.Equal(idx) {
				t.Fatalf("%s: %s 判不等（会每次重建）：db=%+v decl=%+v", label, db.Name, db, idx)
			}
		}
	}
	check("首次")
	if _, err := o.SyncModel("", new(myiJob)); err != nil {
		t.Fatalf("二次 SyncModel: %v", err)
	}
	check("二次")
	_ = utils.ToInt
	_ = dataset.NewDataSet
}
