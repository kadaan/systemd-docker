// Copyright © 2021 Joel Baranick <jbaranick@gmail.com>
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
// 	  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package lib

import (
	"fmt"
	"log"
	"os"
)

type logger struct {
	log *log.Logger
}

func NewLogger() *logger {
	return &logger{
		log: log.New(os.Stderr, "", 0),
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
