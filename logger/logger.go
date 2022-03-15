package logger

import (
	"fmt"

	"github.com/volts-dev/logger"
)

var log = logger.NewLogger(logger.WithPrefix("ORM"))

// build a new logger for entire orm
func init() {
}

// return the logger instance
func Logger() logger.ILogger {
	return log
}

// 断言如果结果和条件不一致就错误
func Assert(cnd bool, format string, args ...interface{}) {
	if !cnd {
		panic(fmt.Sprintf(format, args...))
	}
}

func Panicf(format string, args ...interface{}) {
	panic(fmt.Sprintf(format, args...))
}

func Info(err ...interface{}) {
	log.Info(err...)
}

func Infof(fmt string, arg ...interface{}) {
	log.Infof(fmt, arg...)
}

func Dbg(err ...interface{}) {
	log.Dbg(err...)
}

func Warn(err ...interface{}) {
	log.Warn(err...)
}

func Warnf(fmt string, arg ...interface{}) {
	log.Warnf(fmt, arg...)
}

func Err(err ...interface{}) error {
	return log.Err(err...)
}

func Errf(fmt string, arg ...interface{}) error {
	return log.Errf(fmt, arg...)
}
