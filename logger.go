package orm

import (
	log "github.com/volts-dev/logger"
)

var logger log.ILogger

// build a new logger for entire orm
func init() {
	logger = log.NewLogger(`{"Prefix":"ORM"}`)
}

// return the logger instance
func Logger() log.ILogger {
	return logger
}

func SetLogger(writer log.IWriter) {
	//logger.setWriter(writer)
}
