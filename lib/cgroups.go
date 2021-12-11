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
	"bufio"
	"fmt"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"os"
	"path/filepath"
	"strings"
)

func MoveCgroups(c *Context) error {
	procFile := "/proc/self/cgroup"
	f, err := os.Open(procFile)
	if err != nil {
		return err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	unifiedMode := cgroups.IsCgroup2UnifiedMode()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if err = moveCgroup(c, line, unifiedMode); err != nil {
			return err
		}
	}
	return nil
}

func moveCgroup(c *Context, line string, unifiedMode bool) error {
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
		return fmt.Errorf("cannot parse cgroup line %q", line)
	}

	// root cgroup, skip it
	if parts[2] == "/" {
		return nil
	}

	cgroupRoot := "/sys/fs/cgroup"
	// Special case the unified mount on hybrid cgroup and named hierarchies.
	// This works on Fedora 31, but we should really parse the mounts to see
	// where the cgroup hierarchy is mounted.
	if parts[1] == "" && !unifiedMode {
		// If it is not using unified mode, the cgroup v2 hierarchy is
		// usually mounted under /sys/fs/cgroup/unified
		cgroupRoot = filepath.Join(cgroupRoot, "unified")

		// Ignore the unified mount if it doesn't exist
		if _, err := os.Stat(cgroupRoot); err != nil && os.IsNotExist(err) {
			//continue
		}
	} else if parts[1] != "" {
		// Assume the controller is mounted at /sys/fs/cgroup/$CONTROLLER.
		controller := strings.TrimPrefix(parts[1], "name=")
		cgroupRoot = filepath.Join(cgroupRoot, controller)
	}

	newCgroup := filepath.Join(cgroupRoot, parts[2])
	if err := os.MkdirAll(newCgroup, 0755); err != nil && !os.IsExist(err) {
		return err
	}

	f, err := os.OpenFile(filepath.Join(newCgroup, "cgroup.procs"), os.O_RDWR, 0755)
	if err != nil {
		return err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	if HasPidDied(c.Pid) {
		return nil
	}

	c.Log.Infof("Moving process %d to cgroup %s\n", c.Pid, newCgroup)
	if _, err := f.Write([]byte(fmt.Sprintf("%d\n", c.Pid))); err != nil {
		return fmt.Errorf("Cannot move process %d to cgroup %q: %v\n", c.Pid, newCgroup, err)
	}
	return nil
}
