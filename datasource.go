package orm

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type (
	// provide to connect source
	TDataSource struct {
		DbType   string
		DbName   string
		Host     string
		Port     string
		SSLMode  string
		UserName string
		Password string

		// ApplicationName 是这条连接在库上报的名字（pg_stat_activity.application_name）。
		// 空着的话库上分不清一条连接属于哪个进程。只对 postgres 生效。
		ApplicationName string
		// Timeout 是建立一条新连接的上限（lib/pq 的 connect_timeout，按秒向上取整）。
		// 0 表示不限。只对 postgres 生效。
		Timeout time.Duration
		// BinaryParameters 让 lib/pq 把带参查询的 Parse/Bind/Execute 合成一轮交互
		// （binary_parameters=yes）。事务模式的连接池代理（PgBouncer）要求它；直连
		// 时开不开都能跑，但参数的编码方式会变，开之前要回归 []byte / 时间 / jsonb /
		// 数组。只对 postgres 生效。
		BinaryParameters bool
		// Params 是原样追加到连接串上的其它参数：postgres 下键是 lib/pq 认识的连接参数名；mysql 下是
		// go-sql-driver 的参数或会话变量（值要自己带引号，如 sql_mode: "'…'"），盖过 ORM 的默认值。
		// 不要把 statement_timeout 这类**运行时参数**放进来：它们会变成启动包里的
		// 参数，连接池代理不认；超时设在库或角色上（ALTER DATABASE / ALTER ROLE … SET）。
		Params map[string]string

		// 待定
		Proto   string
		Charset string
		Laddr   string
		Raddr   string
		Schema  string
	}
)

func NewDataSource(dbtype, dbName, host, port, sslMode, userName, password string) *TDataSource {
	return &TDataSource{
		DbType:   dbtype,
		DbName:   dbName,
		Host:     host,
		Port:     port,
		SSLMode:  sslMode,
		UserName: userName,
		Password: password,
	}
}
func (self *TDataSource) validate() error {
	if self.Host == "" {
		self.Host = "127.0.0.1"
	}

	if self.DbType != "sqlite" && self.DbType != "sqlite3" {
		if self.UserName == "" {
			return fmt.Errorf("DataSource request UserName is not blank")
		}

		/*if self.Password == "" {
			return fmt.Errorf("DataSource request Password is not blank")
		}*/
	}

	if self.DbType == "" {
		return fmt.Errorf("DataSource request DbType is not blank")
	}

	if self.DbName == "" {
		return fmt.Errorf("DataSource request DbName is not blank")
	}

	switch strings.ToLower(self.DbType) {
	case "mysql":
		if self.Port == "" {
			self.Port = "3306"
		}
	case "postgres":
		if self.Port == "" {
			self.Port = "5432"
		}
	}

	return nil
}

func (self *TDataSource) toString() (string, error) {
	if err := self.validate(); err != nil {
		return "", err
	}

	var s string
	switch strings.ToLower(self.DbType) {
	case "mysql":
		if self.Host[0] == '/' { // looks like a unix socket
			s = fmt.Sprintf("%s:%s@unix(%s)/%s?%s", self.UserName, self.Password, self.Host, self.DbName, self.mysqlQuery())
		} else {
			s = fmt.Sprintf("%s:%s@tcp(%s)/%s?%s", self.UserName, self.Password, self.hostWithPort(), self.DbName, self.mysqlQuery())
		}

	case "postgres":
		s = fmt.Sprintf("postgres://%s:%s@%s/%s?%s",
			url.QueryEscape(self.UserName), url.QueryEscape(self.Password), self.hostWithPort(), self.DbName, self.postgresQuery())
	case "sqlite", "sqlite3":
		s = self.DbName
	default:
		return "", fmt.Errorf("Unsupported database type: %s", self.DbType)
	}

	return s, nil
}

// MySQLSqlMode 是 ORM 连 MySQL 时固定发的 sql_mode（MySQL 8.0 社区版的默认值）。显式写上是因为
// 实例上的默认值靠不住——阿里云 RDS 的默认 sql_mode 与社区版不同，宽松模式下超长值被**截断**、
// 非法日期落成 0000-00-00，都只是一条警告。Params["sql_mode"] 可以整个换掉它。
const MySQLSqlMode = "ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION"

// mysqlQuery 拼 MySQL 连接串的查询部分（键按字典序，同一份配置逐字节稳定）。
//
//   - charset=utf8mb4：MySQL 的 utf8 是 utf8mb3，存不了 emoji 与部分生僻字。
//   - parseTime + loc=UTC：读回来的 DATETIME 贴 UTC 标签，与 lib/pq 读 timestamp 同形（vectors core/wall
//     的口径：墙上时间贴 UTC 标签）。写入方向由 mysql.BindTime 保证落的是墙上字段。不能用 loc=Local：
//     读回来变成本地标签，与 wall.Now() 比较就差一个时区。
//   - collation_connection='utf8mb4_0900_as_cs'：连接的排序规则，与库 / 列一样区分大小写与重音（2026-10-24
//     定的口径）。不设是驱动默认的 utf8mb4_0900_ai_ci，两个参数之间的比较按它来。走会话变量而不是驱动的
//     collation 参数：go-sql-driver 1.9 的排序规则表里没有 0900_as_cs，直接拒连；会话变量在 SET NAMES 之后设。
//   - sql_mode：见 MySQLSqlMode。
//
// Params 原样追加 / 覆盖（go-sql-driver 把不认识的键当会话变量，值要带引号，这里负责 URL 编码）。
func (self *TDataSource) mysqlQuery() string {
	q := url.Values{}
	q.Set("charset", "utf8mb4")
	q.Set("collation_connection", "'utf8mb4_0900_as_cs'")
	q.Set("parseTime", "true")
	q.Set("loc", "UTC")
	q.Set("sql_mode", "'"+MySQLSqlMode+"'")
	for k, v := range self.Params {
		q.Set(k, v)
	}
	return q.Encode()
}

// hostWithPort 返回连接串里的 host[:port]（postgres 与 mysql 共用）。
//
// Host 里已经带端口（本仓的配置一直写成 "localhost:5432"）就原样用；不带时补上 Port。
// 此前 Port 字段被整个忽略——Host 不带端口时 PG 连的永远是 5432；MySQL 同样（2026-10-24：
// 方言矩阵把测试库开在 33306，ORM 却连去了本机 3306 上另一个 MySQL）。
func (self *TDataSource) hostWithPort() string {
	host := self.Host
	if host == "" || host[0] == '/' { // unix socket 目录：URL 形式表达不了端口，原样交出去
		return host
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if self.Port == "" {
		return host
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), self.Port)
}

// postgresQuery 拼连接串的查询部分。键按字典序输出，同一份配置拼出的串逐字节稳定。
func (self *TDataSource) postgresQuery() string {
	q := url.Values{}
	for k, v := range self.Params {
		q.Set(k, v)
	}
	// 下面这些由专门的字段管，盖过 Params 里的同名键。
	q.Set("sslmode", self.SSLMode)
	if self.ApplicationName != "" {
		q.Set("application_name", self.ApplicationName)
	}
	if self.Timeout > 0 {
		secs := int64((self.Timeout + time.Second - 1) / time.Second)
		q.Set("connect_timeout", strconv.FormatInt(secs, 10))
	}
	if self.BinaryParameters {
		q.Set("binary_parameters", "yes")
	}
	return q.Encode()
}
