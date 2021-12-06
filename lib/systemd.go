package lib

import (
	"errors"
	"fmt"
	"net"
	"os"
)

func Notify(c *Context) error {
	if HasPidDied(c.Pid) {
		return errors.New(fmt.Sprintf("container '%s' exited before we could notify systemd", c.Name))
	}

	if len(c.NotifySocket) == 0 {
		return nil
	}

	conn, err := net.Dial("unixgram", c.NotifySocket)
	if err != nil {
		return err
	}

	_, err = conn.Write([]byte(fmt.Sprintf("MAINPID=%d", c.Pid)))
	if err != nil {
		_ = conn.Close()
		return err
	}

	if HasPidDied(c.Pid) {
		_, _ = conn.Write([]byte(fmt.Sprintf("MAINPID=%d", os.Getpid())))
		_ = conn.Close()
		return errors.New(fmt.Sprintf("container '%s' exited before we could notify systemd", c.Name))
	}

	if !c.Notify {
		m, err := CreateMonitor(c)
		if err != nil {
			return err
		}
		if m == nil {
			defer func(conn net.Conn) {
				_ = conn.Close()
			}(conn)

			if _, err = conn.Write([]byte("READY=1")); err == nil {
				c.Log.Infof("Signaled to systemd that the container '%s' is healthy\n", c.Id)
			} else {
				return err
			}
		} else {
			go func(m Monitor) {
				defer func(m Monitor) {
					_ = m.Close()
				}(m)
				_ = m.Start(conn)
			}(m)
		}
	}

	return nil
}