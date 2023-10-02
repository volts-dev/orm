package orm

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

type mysqlDriver struct {
}

func (p *mysqlDriver) Parse(driverName, dataSourceName string) (*TDataSource, error) {
	dsnPattern := regexp.MustCompile(
		`^(?:(?P<user>.*?)(?::(?P<passwd>.*))?@)?` + // [user[:password]@]
			`(?:(?P<net>[^\(]*)(?:\((?P<addr>[^\)]*)\))?)?` + // [net[(addr)]]
			`\/(?P<dbname>.*?)` + // /dbname
			`(?:\?(?P<params>[^\?]*))?$`) // [?param1=value1&paramN=valueN]
	matches := dsnPattern.FindStringSubmatch(dataSourceName)
	// tlsConfigRegister := make(map[string]*tls.Config)
	names := dsnPattern.SubexpNames()

	uri := &TDataSource{DbType: MYSQL}

	for i, match := range matches {
		switch names[i] {
		case "dbname":
			uri.DbName = match
		case "params":
			if len(match) > 0 {
				kvs := strings.Split(match, "&")
				for _, kv := range kvs {
					splits := strings.Split(kv, "=")
					if len(splits) == 2 {
						if splits[0] == "charset" {
							uri.Charset = splits[1]
						}
					}
				}
			}
		}
	}
	return uri, nil
}

func (p *mysqlDriver) GenScanResult(colType string) (interface{}, error) {
	switch colType {
	case "CHAR", "VARCHAR", "TINYTEXT", "TEXT", "MEDIUMTEXT", "LONGTEXT", "ENUM", "SET":
		var s sql.NullString
		return &s, nil
	case "BIGINT":
		var s sql.NullInt64
		return &s, nil
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT":
		var s sql.NullInt32
		return &s, nil
	case "FLOAT", "REAL", "DOUBLE PRECISION", "DOUBLE":
		var s sql.NullFloat64
		return &s, nil
	case "DECIMAL", "NUMERIC":
		var s sql.NullString
		return &s, nil
	case "DATETIME", "TIMESTAMP":
		var s sql.NullTime
		return &s, nil
	case "BIT":
		var s sql.RawBytes
		return &s, nil
	case "BINARY", "VARBINARY", "TINYBLOB", "BLOB", "MEDIUMBLOB", "LONGBLOB":
		var r sql.RawBytes
		return &r, nil
	default:
		var r sql.RawBytes
		return &r, nil
	}
}

type mymysqlDriver struct {
	mysqlDriver
}

func (p *mymysqlDriver) Parse(driverName, dataSourceName string) (*TDataSource, error) {
	uri := &TDataSource{DbType: MYSQL}

	pd := strings.SplitN(dataSourceName, "*", 2)
	if len(pd) == 2 {
		// Parse protocol part of URI
		p := strings.SplitN(pd[0], ":", 2)
		if len(p) != 2 {
			return nil, errors.New("wrong protocol part of URI")
		}
		uri.Proto = p[0]
		options := strings.Split(p[1], ",")
		uri.Raddr = options[0]
		for _, o := range options[1:] {
			kv := strings.SplitN(o, "=", 2)
			var k, v string
			if len(kv) == 2 {
				k, v = kv[0], kv[1]
			} else {
				k, v = o, "true"
			}
			switch k {
			case "laddr":
				uri.Laddr = v
			case "timeout":
				to, err := time.ParseDuration(v)
				if err != nil {
					return nil, err
				}
				uri.Timeout = to
			default:
				return nil, errors.New("unknown option: " + k)
			}
		}
		// Remove protocol part
		pd = pd[1:]
	}
	// Parse database part of URI
	dup := strings.SplitN(pd[0], "/", 3)
	if len(dup) != 3 {
		return nil, errors.New("Wrong database part of URI")
	}
	uri.DbName = dup[0]
	uri.UserName = dup[1]
	uri.Password = dup[2]

	return uri, nil
}
