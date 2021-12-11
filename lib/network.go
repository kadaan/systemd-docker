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
	"strings"
)

type Networks struct {
	value   *map[string]string
	changed bool
}

func (t *Networks) Len() int {
	if !t.changed {
		return 0
	}
	return len(*t.value)
}

func (t *Networks) Get() map[string]string {
	if !t.changed {
		return make(map[string]string, 0)
	}
	result := make(map[string]string, len(*t.value))
	for key, value := range *t.value {
		result[key] = value
	}
	return result
}

func (t *Networks) Type() string {
	return "network"
}

func (t *Networks) String() string {
	result := ""
	if t.changed {
		for key, value := range *t.value {
			if len(result) > 0 {
				result = fmt.Sprintf("%s,", result)
			}
			result = fmt.Sprintf("%s%s=%s", result, key, value)
		}
	}
	return result
}

func (t *Networks) Set(value string) error {
	if !t.changed {
		value := make(map[string]string, 0)
		t.value = &value
		t.changed = true
	}
	parts := strings.Split(value, ",")
	for _, part := range parts {
		ipAddress := ""
		segment := strings.SplitN(part, ":", 2)
		if len(segment) > 1 {
			ipAddress = strings.TrimSpace(segment[1])
		}
		networkName := strings.TrimSpace(segment[0])
		if networkName == "" {
			return fmt.Errorf("network '%s' has a wrong format", value)
		}
		(*t.value)[networkName] = ipAddress
	}
	return nil
}
