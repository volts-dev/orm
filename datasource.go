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

func (self *TDataSource) validate() {
	if self.Host == "" {
		self.Host = "127.0.0.1"
	}

	if self.UserName == "" {
		panic("DataSource request UserName is not blank!")
	}

	if self.Password == "" {
		panic("DataSource request Password is not blank!")
	}

	if self.DbType == "" {
		panic("DataSource request DbType is not blank!")
	}

	if self.DbName == "" {
		panic("DataSource request DbName is not blank!")
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

}

func (self *TDataSource) toString() string {
	self.validate()

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
	default:
		panic("Unsupported database type: " + self.DbType)
	}

	return s
}
