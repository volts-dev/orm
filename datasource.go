package orm

import (
	"fmt"
	"net/url"
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

		// 待定
		Proto   string
		Charset string
		Laddr   string
		Raddr   string
		Timeout time.Duration
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

		if self.Password == "" {
			return fmt.Errorf("DataSource request Password is not blank")
		}
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
		s = fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%v",
			url.QueryEscape(self.UserName), url.QueryEscape(self.Password), self.Host, self.DbName, self.SSLMode)
	case "sqlite", "sqlite3":
		s = self.DbName
	default:
		return "", fmt.Errorf("Unsupported database type: %s", self.DbType)
	}

	return s, nil
}
