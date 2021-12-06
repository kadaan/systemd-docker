package cmd

import (
	"fmt"
	"github.com/kadaan/systemd-docker/lib"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"os"
	"strconv"
	"strings"
)

var (
	execCmd = &cobra.Command{
		Use:               	   "systemd-docker [flags] -- [docker flags]",
		Short:             	   "systemd-docker is a wrapper for 'docker run' so that you can sanely run Docker containers under systemd.",
		Long:                  `systemd-docker is a wrapper for 'docker run' so that you can sanely run Docker containers under systemd.
Using this wrapper you can manage containers through systemctl or the docker CLI.
Additionally you can leverage all the cgroup functionality of systemd and systemd-notify.`,
		Example: 			   `systemd-docker --pid-file=/tmp/registry-pid --networks mqtt_proxy,prometheus_proxy:192.168.98.4 -- 
    --name registry --publish 5000:5000 --env 'REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY=/data' pihost/registry:latest`,
		RunE:      		   	   run,
		Version:			   "1.0.0",
		DisableFlagsInUseLine: true,
	}
	c = &lib.Context{
		Log:        lib.NewLogger(),
		AllCgroups: false,
	}
)

func init() {
	execCmd.Flags().StringVarP(&c.PidFile, "pid-file", "p",  "", "Path to write PID of container to")
	execCmd.Flags().BoolVarP(&c.Logs, "logs", "l", true, "Enable log piping")
	execCmd.Flags().BoolVarP(&c.Notify, "notify", "n", false, "Setup systemd notify for container")
	execCmd.Flags().BoolVarP(&c.Env, "env", "e", false, "Inherit environment variables")
	execCmd.Flags().BoolVar(&c.UnifiedHiearchy, "unified-hierarchy", false, "Use the unified CGroupV2 hierarchy at /sys/fs/cgroup/unified")
	execCmd.Flags().StringSliceVarP(&c.Cgroups, "cgroups", "c", []string{}, "CGroups to take ownership of or 'all' for all CGroups available")
	execCmd.Flags().Var(&c.Networks, "networks", "Networks to join, <NETWORK_NAME>[:<IP_ADDRESS>]")
}

func run(cmd *cobra.Command, args []string) error {
	newArgs := make([]string, 0, len(args))

	for i, arg := range args {
		add := true

		switch {
		case strings.HasPrefix(arg, "-rm") || strings.HasPrefix(arg, "--rm"):
			if strings.Contains(arg, "=") {
				if rm, err := strconv.ParseBool(strings.SplitN(arg, "=", 2)[1]); err != nil {
					return errors.Errorf("")
				} else if rm {
					c.Rm = true
				}
			} else {
				c.Rm = true
			}
			add = false
		case arg == "-d" || arg == "-detach" || arg == "--detach":
			c.Log.Warnf("docker flag 'detach' is ignored")
			add = false
		case strings.HasPrefix(arg, "-name") || strings.HasPrefix(arg, "--name"):
			if strings.Contains(arg, "=") {
				c.Name = strings.SplitN(arg, "=", 2)[1]
			} else if len(args) > i+1 {
				c.Name = args[i+1]
			}
		}

		if add {
			newArgs = append(newArgs, arg)
		}
	}

	if len(c.Name) == 0 {
		return fmt.Errorf("required docker flag 'name' is not set")
	}

	c.NotifySocket = os.Getenv("NOTIFY_SOCKET")
	c.Args = newArgs

	for _, val := range c.Cgroups {
		if val == "all" {
			c.Cgroups = nil
			c.AllCgroups = true
			break
		}
	}

	var autoArgs []string
	if c.Notify {
		if len(c.NotifySocket) > 0 {
			autoArgs = append(autoArgs, "-e", fmt.Sprintf("NOTIFY_SOCKET=%s", c.NotifySocket))
			autoArgs = append(autoArgs, "-v", fmt.Sprintf("%s:%s", c.NotifySocket, c.NotifySocket))
		} else {
			c.Log.Warnf("No NOTIFY_SOCKET found, 'notify' flag will have no effect")
		}
	} else {
		c.Notify = false
	}

	if c.Env {
		for _, val := range os.Environ() {
			if !strings.HasPrefix(val, "HOME=") && !strings.HasPrefix(val, "PATH=") {
				autoArgs = append(autoArgs, "-e", val)
			}
		}
	}

	if len(autoArgs) > 0 {
		c.Args = append(autoArgs, c.Args...)
	}

	err := lib.RunContainer(c)
	if err != nil {
		return err
	}

	_, err = lib.MoveCgroups(c)
	if err != nil {
		return err
	}

	err = lib.Notify(c)
	if err != nil {
		return err
	}

	err = lib.WritePidFile(c)
	if err != nil {
		return err
	}

	go func() {
		_ = lib.PipeLogs(c)
	}()

	err = lib.WaitForContainerExit(c)
	if err != nil {
		return err
	}

	err = lib.RemoveContainer(c)
	if err != nil {
		return err
	}

	return nil
}

func Execute() {
	if err := execCmd.Execute(); err != nil {
		os.Exit(1)
	}
}