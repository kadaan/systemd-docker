package lib

import (
	"errors"
	"fmt"
	dockerClient "github.com/fsouza/go-dockerclient"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

func lookupNamedContainer(c *Context) error {
	client, err := c.GetClient()
	if err != nil {
		return err
	}

	containerOptions := dockerClient.InspectContainerOptions{ID: c.Name}
	container, err := client.InspectContainerWithOptions(containerOptions)
	if _, ok := err.(*dockerClient.NoSuchContainer); ok {
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
		return client.RemoveContainer(dockerClient.RemoveContainerOptions{
			ID:    container.ID,
			Force: true,
		})
	} else {
		client, err := c.GetClient()
		err = client.StartContainer(container.ID, container.HostConfig)
		if err != nil {
			return err
		}

		container, err = client.InspectContainerWithOptions(containerOptions)
		if err != nil {
			return err
		}

		c.Id = container.ID
		c.Pid = container.State.Pid

		return nil
	}
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
	err := c.Cmd.Start()
	if err != nil {
		return err
	}

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
		args = append(args, name, c.Name)
		c.Cmd = exec.Command(dockerCommand, args...)
		err := c.Cmd.Start()
		if err != nil {
			return err
		}

		err = c.Cmd.Wait()
		if err != nil {
			return err
		}

		if !c.Cmd.ProcessState.Success() {
			return err
		}

		c.Log.Infof("Container '%s' joined network '%s' using %s\n", c.Name, name, ipMessage)
	}

	return nil
}

func startContainer(c *Context) error {
	dockerCommand := getDockerCommand()

	c.Cmd = exec.Command(dockerCommand, "start", c.Name)

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

	c.Pid, err = getContainerPid(c)

	return err
}

func getContainerPid(c *Context) (int, error) {
	client, err := c.GetClient()
	if err != nil {
		return 0, err
	}

	container, err := client.InspectContainerWithOptions(dockerClient.InspectContainerOptions{ID: c.Id})
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

func RunContainer(c *Context) error {
	if len(c.Name) > 0 {
		err := lookupNamedContainer(c)
		if err != nil {
			return err
		}
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

		err = startContainer(c)
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
	if c.Logs || c.Rm {
		client, err := c.GetClient()
		if err != nil {
			return err
		}

		/* Good old polling... */
		containerOptions := dockerClient.InspectContainerOptions{ID: c.Id}
		for true {
			container, err := client.InspectContainerWithOptions(containerOptions)
			if err != nil {
				return err
			}

			if container.State.Running {
				_, _ = client.WaitContainer(c.Id)
			} else {
				return nil
			}
		}
	}

	return nil
}

func RemoveContainer(c *Context) error {
	if !c.Rm {
		return nil
	}

	client, err := c.GetClient()
	if err != nil {
		return err
	}

	return client.RemoveContainer(dockerClient.RemoveContainerOptions{
		ID:    c.Id,
		Force: true,
	})
}

func PipeLogs(c *Context) error {
	if !c.Logs {
		return nil
	}

	client, err := c.GetClient()
	if err != nil {
		return err
	}

	err = client.Logs(dockerClient.LogsOptions{
		Container:    c.Id,
		Follow:       true,
		Stdout:       true,
		Stderr:       true,
		OutputStream: os.Stdout,
		ErrorStream:  os.Stderr,
	})

	return err
}