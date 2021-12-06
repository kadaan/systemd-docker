package lib

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
)

func HasPidDied(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return os.IsNotExist(err)
}

func WritePidFile(c *Context) error {
	if len(c.PidFile) == 0 || c.Pid <= 0 {
		return nil
	}

	err := ioutil.WriteFile(c.PidFile, []byte(strconv.Itoa(c.Pid)), 0644)
	if err != nil {
		return err
	}

	return nil
}