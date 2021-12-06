package lib

import (
	"fmt"
	golog "log"
	"os"
)

type logger struct {
	log *golog.Logger
}

func NewLogger() *logger {
	return &logger{
		log: golog.New(os.Stderr, "", 0),
	}
}

func (l *logger) printf(priority int, format string, v ...interface{}) {
	l.log.Printf(fmt.Sprintf("<%d>%s", priority, format), v...)
}

func (l *logger) Fatal(v ...interface{}) {
	l.log.Fatal(v...)
}

func (l *logger) Errorf(format string, v ...interface{}) {
	l.printf(3, format, v...)
}

func (l *logger) Warnf(format string, v ...interface{}) {
	l.printf(4, format, v...)
}

func (l *logger) Noticef(format string, v ...interface{}) {
	l.printf(5, format, v...)
}

func (l *logger) Infof(format string, v ...interface{}) {
	l.printf(6, format, v...)
}

func (l *logger) Debugf(format string, v ...interface{}) {
	l.printf(7, format, v...)
}
