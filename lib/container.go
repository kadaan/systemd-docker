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
	"errors"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

func RunContainer(c *Context) error {
	err := lookupNamedContainer(c)
	if err != nil {
		return err
	}

	if len(c.Id) == 0 {
		err := createContainer(c)
		if err != nil {
			return err
		}

		err = joinNetworks(c)
		if err != nil {
			return err
		}
	}

	if c.Pid == 0 {
		err := startContainer(c)
		if err != nil {
			return err
		}
	}

	if c.Pid == 0 {
		return errors.New("failed to launch container, pid is 0")
	}

	return nil
}

func WaitForContainerExit(c *Context) error {
	c.Log.Infof("Waiting for container '%s' to exit\n", c.Name)

	client, err := c.GetClient()
	if err != nil {
		return err
	}

	containerOptions := docker.InspectContainerOptions{ID: c.Id}
	for true {
		container, err := client.InspectContainerWithOptions(containerOptions)
		if err != nil {
			return err
		}

		if container.State.Running {
			break
		} else {
			c.Log.Infof("Container '%s' is not running\n", c.Name)
			return nil
		}
	}

	listener := make(chan *docker.APIEvents)

	eventsOptions := docker.EventsOptions{
		Filters: map[string][]string{
			"type":      {"container"},
			"container": {c.Id},
			"event":     {"die"},
		},
	}

	if err = client.AddEventListenerWithOptions(eventsOptions, listener); err != nil {
		return err
	}
	defer func() { _ = client.RemoveEventListener(listener) }()

	for {
		select {
		case ev, ok := <-listener:
			if !ok || ev == nil {
				return errors.New("event listener closed")
			}
			if ev.Action == "die" {
				c.Log.Infof("Container '%s' has stopped\n", c.Name)
				return nil
			}
		}
	}
}

func RemoveContainer(c *Context) error {
	if !c.Rm {
		return nil
	}

	client, err := c.GetClient()
	if err != nil {
		return err
	}

	return client.RemoveContainer(docker.RemoveContainerOptions{
		ID:    c.Id,
		Force: true,
	})
}

func lookupNamedContainer(c *Context) error {
	client, err := c.GetClient()
	if err != nil {
		return err
	}

	containerOptions := docker.InspectContainerOptions{ID: c.Name}
	container, err := client.InspectContainerWithOptions(containerOptions)
	if _, ok := err.(*docker.NoSuchContainer); ok {
		return nil
	}
	if err != nil || container == nil {
		return err
	}

	if container.State.Running {
		c.Id = container.ID
		c.Pid = container.State.Pid
		return nil
	} else if c.Rm {
		return client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    container.ID,
			Force: true,
		})
	}
	return nil
}

func getDockerCommand() string {
	dockerCommand := os.Getenv("DOCKER_COMMAND")
	if len(dockerCommand) == 0 {
		dockerCommand = "docker"
	}
	return dockerCommand
}

func createContainer(c *Context) error {
	args := append([]string{"create"}, c.Args...)
	dockerCommand := getDockerCommand()

	c.Cmd = exec.Command(dockerCommand, args...)

	errorPipe, err := c.Cmd.StderrPipe()
	if err != nil {
		return err
	}

	outputPipe, err := c.Cmd.StdoutPipe()
	if err != nil {
		return err
	}

	err = c.Cmd.Start()
	if err != nil {
		return err
	}

	go func() {
		_, _ = io.Copy(os.Stderr, errorPipe)
	}()

	bytes, err := ioutil.ReadAll(outputPipe)
	if err != nil {
		return err
	}

	c.Id = strings.TrimSpace(string(bytes))

	err = c.Cmd.Wait()
	if err != nil {
		return err
	}

	if !c.Cmd.ProcessState.Success() {
		return err
	}

	return nil
}

func joinNetworks(c *Context) error {
	dockerCommand := getDockerCommand()
	for name, ipAddress := range c.Networks.Get() {
		args := []string{
			"network",
			"connect",
		}

		var ipMessage string
		if len(ipAddress) == 0 {
			ipMessage = "dhcp"
		} else {
			ipMessage = fmt.Sprintf("IP %s", ipAddress)
			args = append(args, "--ip", ipAddress)
		}
		args = append(args, name, c.Id)
		c.Cmd = exec.Command(dockerCommand, args...)

		errorPipe, err := c.Cmd.StderrPipe()
		if err != nil {
			return err
		}

		outputPipe, err := c.Cmd.StdoutPipe()
		if err != nil {
			return err
		}

		err = c.Cmd.Start()
		if err != nil {
			return err
		}

		go func() {
			_, _ = io.Copy(os.Stdout, outputPipe)
		}()

		go func() {
			_, _ = io.Copy(os.Stderr, errorPipe)
		}()

		err = c.Cmd.Wait()
		if err != nil {
			return err
		}

		if !c.Cmd.ProcessState.Success() {
			return err
		}

		c.Log.Infof("Container '%s' joined network '%s' with %s\n", c.Name, name, ipMessage)
	}

	return nil
}

func startContainer(c *Context) error {
	dockerCommand := getDockerCommand()
	c.Cmd = exec.Command(dockerCommand, "start", c.Id)

	errorPipe, err := c.Cmd.StderrPipe()
	if err != nil {
		return err
	}

	outputPipe, err := c.Cmd.StdoutPipe()
	if err != nil {
		return err
	}

	err = c.Cmd.Start()
	if err != nil {
		return err
	}

	go func() {
		_, _ = io.Copy(os.Stdout, outputPipe)
	}()

	go func() {
		_, _ = io.Copy(os.Stderr, errorPipe)
	}()

	err = c.Cmd.Wait()
	if err != nil {
		return err
	}

	if !c.Cmd.ProcessState.Success() {
		return err
	}

	c.Pid, err = getContainerPid(c)

	return err
}

func getContainerPid(c *Context) (int, error) {
	client, err := c.GetClient()
	if err != nil {
		return 0, err
	}

	container, err := client.InspectContainerWithOptions(docker.InspectContainerOptions{ID: c.Id})
	if err != nil {
		return 0, err
	}

	if container == nil {
		return 0, errors.New(fmt.Sprintf("Failed to find container '%s'", c.Id))
	}

	if container.State.Pid <= 0 {
		return 0, errors.New(fmt.Sprintf("Pid is %d for container '%s'", container.State.Pid, c.Id))
	}

	return container.State.Pid, nil
}
