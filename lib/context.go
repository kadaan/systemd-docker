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
	dockerClient "github.com/fsouza/go-dockerclient"
	"os"
	"os/exec"
)

type Context struct {
	Args          []string
	Cgroups       []string
	AllCgroups    bool
	Logs          bool
	Notify        bool
	Action        string
	Name          string
	Env           bool
	Rm            bool
	Id            string
	NotifySocket  string
	Cmd           *exec.Cmd
	Pid           int
	PidFile       string
	client        *dockerClient.Client
	Networks      Networks
	Log           *logger
	PrintVersion  bool
	CpuProfile    string
	MemoryProfile string
	TraceProfile  string
}

func (c *Context) GetClient() (*dockerClient.Client, error) {
	var err error
	if c.client == nil {
		endpoint := os.Getenv("DOCKER_HOST")
		if len(endpoint) == 0 {
			endpoint = "unix:///var/run/docker.sock"
		}

		c.client, err = dockerClient.NewClient(endpoint)
	}

	return c.client, err
}
