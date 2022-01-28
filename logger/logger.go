package logger

import (
	"fmt"

	log "github.com/volts-dev/logger"
)

var logger log.ILogger

// build a new logger for entire orm
func init() {
	logger = log.NewLogger(log.WithPrefix(`{"Prefix":"ORM"}`))
}

// return the logger instance
func Logger() log.ILogger {
	return logger
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
	logger.Info(err...)
}

func Infof(fmt string, arg ...interface{}) {
	logger.Infof(fmt, arg...)
}

func Dbg(err ...interface{}) {
	logger.Dbg(err...)
}

func Warn(err ...interface{}) {
	logger.Warn(err...)
}

func Warnf(fmt string, arg ...interface{}) {
	logger.Warnf(fmt, arg...)
}

func Err(err ...interface{}) error {
	return logger.Err(err...)
}

func Errf(fmt string, arg ...interface{}) error {
	return logger.Errf(fmt, arg...)
}
