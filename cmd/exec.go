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

package cmd

import (
	"fmt"
	"github.com/kadaan/systemd-docker/lib"
	"github.com/kadaan/systemd-docker/version"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
)

// TODO: Add flag for https://github.com/weaveworks/prom-aggregation-gateway url and push
//       counter for starts, failures, etc.  Then we can alert on flapping services.
var (
	rootCmd = &cobra.Command{
		Use:   "systemd-docker [flags] -- [docker flags]",
		Short: "systemd-docker is a wrapper for 'docker run' so that you can sanely run Docker containers under systemd.",
		Long: `systemd-docker is a wrapper for 'docker run' so that you can sanely run Docker containers under systemd.
Using this wrapper you can manage containers through systemctl or the docker CLI.
Additionally you can leverage all the cgroup functionality of systemd and systemd-notify.`,
		Example: `systemd-docker --pid-file=/tmp/registry-pid --networks mqtt_proxy,prometheus_proxy:192.168.98.4 -- 
    --name registry --publish 5000:5000 --env 'REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY=/data' registry:latest`,
		PreRun:                pre,
		RunE:                  run,
		DisableFlagsInUseLine: true,
	}
	c = &lib.Context{
		Log:        lib.NewLogger(),
		AllCgroups: false,
	}
)

func init() {
	rootCmd.SetVersionTemplate(version.Print())
	rootCmd.Flags().StringVarP(&c.PidFile, "pid-file", "p", "", "Path to write PID of container to")
	rootCmd.Flags().BoolVarP(&c.Logs, "logs", "l", true, "Enable log piping")
	rootCmd.Flags().BoolVarP(&c.Notify, "notify", "n", false, "Setup systemd notify for container")
	rootCmd.Flags().BoolVarP(&c.Env, "env", "e", false, "Inherit environment variables")
	rootCmd.Flags().StringSliceVarP(&c.Cgroups, "cgroups", "c", []string{}, "CGroups to take ownership of or 'all' for all CGroups available")
	rootCmd.Flags().Var(&c.Networks, "networks", "Networks to join, <NETWORK_NAME>[:<IP_ADDRESS>]")
	rootCmd.Flags().StringVar(&c.CpuProfile, "cpuProfile", "", "Cpu profile result file")
	rootCmd.Flags().StringVar(&c.MemoryProfile, "memoryProfile", "", "Memory profile result file")
	rootCmd.Flags().StringVar(&c.TraceProfile, "traceProfile", "", "Trace profile result file")
	rootCmd.Flags().BoolVar(&c.PrintVersion, "version", false, "Print version")
}

func pre(_ *cobra.Command, _ []string) {
	if c.PrintVersion {
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", version.Print())
		os.Exit(0)
	}
}

func run(_ *cobra.Command, args []string) error {
	if c.TraceProfile != "" {
		f, err := os.Create(c.TraceProfile)
		if err != nil {
			c.Log.Fatal("Could not create Trace profile: ", err)
		}
		defer func(f *os.File) {
			_ = f.Close()
		}(f)
		if err := trace.Start(f); err != nil {
			c.Log.Fatal("Could not start Trace profile: ", err)
		}
		defer trace.Stop()
	}

	if c.CpuProfile != "" {
		f, err := os.Create(c.CpuProfile)
		if err != nil {
			c.Log.Fatal("Could not create CPU profile: ", err)
		}
		defer func(f *os.File) {
			_ = f.Close()
		}(f)
		if err := pprof.StartCPUProfile(f); err != nil {
			c.Log.Fatal("Could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if c.MemoryProfile != "" {
		f, err := os.Create(c.MemoryProfile)
		if err != nil {
			c.Log.Fatal("Could not create memory profile: ", err)
		}
		defer func(f *os.File) {
			defer func(f *os.File) {
				_ = f.Close()
			}(f)
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				c.Log.Fatal("Could not write memory profile: ", err)
			}
		}(f)
	}

	newArgs := make([]string, 0, len(args))

	logTagSpecified := false
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
		case strings.HasPrefix(arg, "-log-driver") || strings.HasPrefix(arg, "--log-driver"):
			c.Log.Warnf("docker flag 'log-driver' is ignored")
			add = false
		case strings.HasPrefix(arg, "-log-opt") || strings.HasPrefix(arg, "--log-opt"):
			var value string
			if strings.Contains(arg, "=") {
				value = strings.SplitN(arg, "=", 2)[1]
			} else if len(args) > i+1 {
				value = args[i+1]
			}
			if strings.HasPrefix(value, "tag=") {
				logTagSpecified = true
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
	if c.Logs {
		autoArgs = append(autoArgs, "--log-driver", "journald")
		if !logTagSpecified {
			autoArgs = append(autoArgs, "--log-opt", fmt.Sprintf("tag=%s", c.Name))
		}
	}
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

	err = lib.MoveCgroups(c)
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
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
