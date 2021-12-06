package lib

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
)

var (
	CgroupProcs     = "cgroup.procs"
	SysFsCgroupPath = "/sys/fs/cgroup"
	ProcCgroupPath  = "/proc/%d/cgroup"
)

func getCgroupsForPid(pid int) (map[string]string, error) {
	file, err := os.Open(fmt.Sprintf(ProcCgroupPath, pid))
	if err != nil {
		return nil, err
	}

	ret := map[string]string{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.SplitN(scanner.Text(), ":", 3)
		if len(line) != 3 || line[1] == "" {
			continue
		}

		// For cgroups like "cpu,cpuacct" the ordering isn't guaranteed to be
		// the same as the /sys/fs/cgroup dentry. But the kernel provides symlinks
		// for each of the comma-separated components.
		for _, part := range strings.Split(line[1], ",") {
			ret[part] = line[2]
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return ret, nil
}

func getCgroupPids(c *Context, cgroupName string, cgroupPath string) ([]string, error) {
	var ret []string

	file, err := os.Open(constructCgroupPath(c, cgroupName, cgroupPath))
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		ret = append(ret, strings.TrimSpace(scanner.Text()))
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}

	return ret, nil
}

func constructCgroupPath(c *Context, cgroupName string, cgroupPath string) string {
	if cgroupName == "" && c.UnifiedHiearchy {
		cgroupName = "unified"
	}
	return path.Join(SysFsCgroupPath, strings.TrimPrefix(cgroupName, "name="), cgroupPath, CgroupProcs)
}

func writePid(pid string, path string) error {
	return ioutil.WriteFile(path, []byte(pid), 0644)
}

func MoveCgroups(c *Context) (bool, error) {
	moved := false
	currentCgroups, err := getCgroupsForPid(os.Getpid())
	if err != nil {
		return false, err
	}

	containerCgroups, err := getCgroupsForPid(c.Pid)
	if err != nil {
		return false, err
	}

	var ns []string

	if c.AllCgroups || c.Cgroups == nil || len(c.Cgroups) == 0 {
		ns = make([]string, 0, len(containerCgroups))
		for value := range containerCgroups {
			ns = append(ns, value)
		}
	} else {
		ns = c.Cgroups
	}

	for _, nsName := range ns {
		currentPath, ok := currentCgroups[nsName]
		if !ok {
			continue
		}

		containerPath, ok := containerCgroups[nsName]
		if !ok {
			continue
		}

		if currentPath == containerPath || containerPath == "/" {
			continue
		}

		pids, err := getCgroupPids(c, nsName, containerPath)
		if err != nil {
			return false, err
		}

		for _, pid := range pids {
			pidInt, err := strconv.Atoi(pid)
			if err != nil {
				continue
			}

			if HasPidDied(pidInt) {
				continue
			}

			currentFullPath := constructCgroupPath(c, nsName, currentPath)
			c.Log.Infof("Moving pid %s to %s\n", pid, currentFullPath)
			err = writePid(pid, currentFullPath)
			if err != nil {
				if HasPidDied(pidInt) {
					// Ignore if PID died between previous check and cgroup assignment
					continue
				}
				return false, err
			}

			moved = true
		}
	}

	return moved, nil
}