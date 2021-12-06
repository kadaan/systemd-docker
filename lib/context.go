package lib

import (
	dockerClient "github.com/fsouza/go-dockerclient"
	"os"
	"os/exec"
)

type Context struct {
	Args            []string
	Cgroups         []string
	AllCgroups      bool
	UnifiedHiearchy bool
	LogFilters      []string
	Logs            bool
	Notify          bool
	Action          string
	Name            string
	Env             bool
	Rm              bool
	Id              string
	NotifySocket    string
	Cmd             *exec.Cmd
	Pid             int
	PidFile         string
	client          *dockerClient.Client
	Networks        Networks
	Log             *logger
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
