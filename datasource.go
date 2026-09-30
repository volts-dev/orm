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
		// Params 是原样追加到连接串上的其它参数，键是 lib/pq 认识的连接参数名。
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
			s = fmt.Sprintf("%s:%s@unix(%s)/%s?charset=utf8&parseTime=true",
				self.UserName, self.Password, self.Host, self.DbName)
		} else {
			s = fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8&parseTime=true",
				self.UserName, self.Password, self.Host, self.DbName)
		}

	case "postgres":
		s = fmt.Sprintf("postgres://%s:%s@%s/%s?%s",
			url.QueryEscape(self.UserName), url.QueryEscape(self.Password), self.postgresHost(), self.DbName, self.postgresQuery())
	case "sqlite", "sqlite3":
		s = self.DbName
	default:
		return "", fmt.Errorf("Unsupported database type: %s", self.DbType)
	}

	return s, nil
}

// postgresHost 返回连接串里的 host[:port]。
//
// Host 里已经带端口（本仓的配置一直写成 "localhost:5432"）就原样用；不带时补上 Port。
// 此前 Port 字段被整个忽略——Host 不带端口时连的永远是 5432，改 Port 没有任何效果。
func (self *TDataSource) postgresHost() string {
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
