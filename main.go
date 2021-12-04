package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/docker/docker/opts"
	dockerClient "github.com/fsouza/go-dockerclient"
	flag "github.com/weaveworks/common/mflag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

var (
	CgroupProcs     = "cgroup.procs"
	SysFsCgroupPath = "/sys/fs/cgroup"
	ProcCgroupPath  = "/proc/%d/cgroup"
)

type Context struct {
	Args            []string
	Cgroups         []string
	AllCgroups      bool
	UnifiedHiearchy bool
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
	Client          *dockerClient.Client
}

type monitor interface {
	Close() error
	Start(conn net.Conn) error
}

type Monitor struct {
	context	 *Context
	client   *dockerClient.Client
	listener chan *dockerClient.APIEvents
}

func (m *Monitor) Start(conn net.Conn) error {
	log.Printf("Starting health check monitor for container %s\n", m.context.Id)
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)
	ready := false
	for {
		select {
		case ev, ok := <-m.listener:
			if !ok || ev == nil {
				return errors.New("event listener closed")
			}
			if strings.HasPrefix(ev.Action, "health_status: ") {
				if ev.Status == "health_status: healthy" {
					if !ready {
						if _, err := conn.Write([]byte("READY=1")); err == nil {
							log.Printf("Signaled to systemd that the container %s is healthy\n", m.context.Id)
							ready = true
						} else {
							fmt.Printf("Failed to signal to systemd that the container %s is healthy: %s\n", m.context.Id, err)
						}
					} else {
						if _, err := conn.Write([]byte("WATCHDOG=1")); err == nil {
							log.Printf("Signaled to systemd watchdog that the container %s is still healthy\n", m.context.Id)
						} else {
							fmt.Printf("Failed to signal to systemd watchdog that the container %s is still healthy: %s\n", m.context.Id, err)
						}
					}
				}
			} else if ev.Action == "stop" || ev.Action == "kill" || ev.Action == "die" || ev.Action == "oom" {
				log.Printf("Container %s is exiting, stopping health check monitor\n", m.context.Id)
				return nil
			}
		}
	}
}

func (m *Monitor) Close() error {
	log.Printf("Closing health check monitor for container %s\n", m.context.Id)
	if err := m.client.RemoveEventListener(m.listener); err != nil {
		return err
	}
	return nil
}

func setupEnvironment(c *Context) {
	var newArgs []string
	if c.Notify && len(c.NotifySocket) > 0 {
		newArgs = append(newArgs, "-e", fmt.Sprintf("NOTIFY_SOCKET=%s", c.NotifySocket))
		newArgs = append(newArgs, "-v", fmt.Sprintf("%s:%s", c.NotifySocket, c.NotifySocket))
	} else {
		c.Notify = false
	}

	if c.Env {
		for _, val := range os.Environ() {
			if !strings.HasPrefix(val, "HOME=") && !strings.HasPrefix(val, "PATH=") {
				newArgs = append(newArgs, "-e", val)
			}
		}
	}

	if len(newArgs) > 0 {
		c.Args = append(newArgs, c.Args...)
	}
}

func parseContext(args []string) (*Context, error) {
	c := &Context{
		Logs:       true,
		AllCgroups: false,
	}

	flags := flag.NewFlagSet("systemd-docker", flag.ContinueOnError)

	flCgroups := opts.NewListOpts(nil)

	flags.StringVar(&c.PidFile, []string{"p", "-pid-file"}, "", "pipe file")
	flags.BoolVar(&c.Logs, []string{"l", "-logs"}, true, "pipe logs")
	flags.BoolVar(&c.Notify, []string{"n", "-notify"}, false, "setup systemd notify for container")
	flags.BoolVar(&c.Env, []string{"e", "-env"}, false, "inherit environment variable")
	flags.BoolVar(&c.UnifiedHiearchy, []string{"-unified-hierarchy"}, false, "use the unified cgroupv2 hiearchy at /sys/fs/cgroup/unified")
	flags.Var(&flCgroups, []string{"c", "-cgroups"}, "cgroups to take ownership of or 'all' for all cgroups available")

	err := flags.Parse(args)
	if err != nil {
		return nil, err
	}

	foundD := false
	var name string

	runArgs := flags.Args()
	if len(runArgs) == 0 || (runArgs[0] != "run" && runArgs[0] != "start") {
		log.Println("Args:", runArgs)
		return nil, errors.New("run/start action not found in arguments")
	}

	c.Action = runArgs[0]
	runArgs = runArgs[1:]
	newArgs := make([]string, 0, len(runArgs))

	for i, arg := range runArgs {
		/* This is tedious, but flag can't ignore unknown flags and I don't want to define them all */
		add := true

		switch {
		case arg == "-rm" || arg == "--rm":
			c.Rm = true
			add = false
		case arg == "-d" || arg == "-detach" || arg == "--detach":
			foundD = true
		case strings.HasPrefix(arg, "-name") || strings.HasPrefix(arg, "--name"):
			if strings.Contains(arg, "=") {
				name = strings.SplitN(arg, "=", 2)[1]
			} else if len(runArgs) > i+1 {
				name = runArgs[i+1]
			}
		}

		if add {
			newArgs = append(newArgs, arg)
		}
	}

	if !foundD && c.Action == "run" {
		newArgs = append([]string{"-d"}, newArgs...)
	}

	c.Name = name
	c.NotifySocket = os.Getenv("NOTIFY_SOCKET")
	c.Args = newArgs
	c.Cgroups = flCgroups.GetAll()

	for _, val := range c.Cgroups {
		if val == "all" {
			c.Cgroups = nil
			c.AllCgroups = true
			break
		}
	}

	setupEnvironment(c)

	return c, nil
}

func lookupNamedContainer(c *Context) error {
	client, err := getClient(c)
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
		client, err := getClient(c)
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

func launchContainer(c *Context) error {
	args := append([]string{c.Action}, c.Args...)
	dockerCommand := os.Getenv("DOCKER_COMMAND")
	if len(dockerCommand) == 0 {
		dockerCommand = "docker"
	}

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

	c.Pid, err = getContainerPid(c)

	return err
}

func runContainer(c *Context) error {
	if len(c.Name) > 0 {
		err := lookupNamedContainer(c)
		if err != nil {
			return err
		}

	}

	if len(c.Id) == 0 {
		err := launchContainer(c)
		if err != nil {
			return err
		}
	}

	if c.Pid == 0 {
		return errors.New("failed to launch container, pid is 0")
	}

	return nil
}

func getClient(c *Context) (*dockerClient.Client, error) {
	if c.Client != nil {
		return c.Client, nil
	}

	endpoint := os.Getenv("DOCKER_HOST")
	if len(endpoint) == 0 {
		endpoint = "unix:///var/run/docker.sock"
	}

	return dockerClient.NewClient(endpoint)
}

func getContainerPid(c *Context) (int, error) {
	client, err := getClient(c)
	if err != nil {
		return 0, err
	}

	container, err := client.InspectContainerWithOptions(dockerClient.InspectContainerOptions{ID: c.Id})
	if err != nil {
		return 0, err
	}

	if container == nil {
		return 0, errors.New(fmt.Sprintf("Failed to find container %s", c.Id))
	}

	if container.State.Pid <= 0 {
		return 0, errors.New(fmt.Sprintf("Pid is %d for container %s", container.State.Pid, c.Id))
	}

	return container.State.Pid, nil
}

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

func constructCgroupPath(c *Context, cgroupName string, cgroupPath string) string {
	if cgroupName == "" && c.UnifiedHiearchy {
		cgroupName = "unified"
	}
	return path.Join(SysFsCgroupPath, strings.TrimPrefix(cgroupName, "name="), cgroupPath, CgroupProcs)
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

func writePid(pid string, path string) error {
	return ioutil.WriteFile(path, []byte(pid), 0644)
}

func moveCgroups(c *Context) (bool, error) {
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

			if pidDied(pidInt) {
				continue
			}

			currentFullPath := constructCgroupPath(c, nsName, currentPath)
			log.Printf("Moving pid %s to %s\n", pid, currentFullPath)
			err = writePid(pid, currentFullPath)
			if err != nil {
				if pidDied(pidInt) {
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

func pidDied(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return os.IsNotExist(err)
}

func notify(c *Context) error {
	if pidDied(c.Pid) {
		return errors.New("container exited before we could notify systemd")
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

	if pidDied(c.Pid) {
		_, _ = conn.Write([]byte(fmt.Sprintf("MAINPID=%d", os.Getpid())))
		_ = conn.Close()
		return errors.New("container exited before we could notify systemd")
	}

	if c.Notify {
		m, err := createMonitor(c)
		if err != nil {
			return err
		}
		go func(m monitor) {
			defer func(m monitor) {
				_ = m.Close()
			}(m)
			_ = m.Start(conn)
		}(m)
	} else {
		defer func(conn net.Conn) {
			_ = conn.Close()
		}(conn)

		_, err = conn.Write([]byte("READY=1"))
		if err != nil {
			return err
		}
	}

	return nil
}

func createMonitor(c *Context) (monitor, error) {
	log.Printf("Creating health check monitor for container %s\n", c.Id)

	client, err := getClient(c)
	if err != nil {
		return nil, err
	}

	listener := make(chan *dockerClient.APIEvents)
	eventsOptions := dockerClient.EventsOptions{
		Filters: map[string][]string{
			"type": {"container"},
			"container": {c.Id},
			"event": {"health_status", "stop", "kill", "die", "oom"},
		},
	}

	if err = client.AddEventListenerWithOptions(eventsOptions, listener); err != nil {
		return nil, err
	}

	return &Monitor{
		context: c,
		client: client,
		listener: listener,
	}, nil
}

func pidFile(c *Context) error {
	if len(c.PidFile) == 0 || c.Pid <= 0 {
		return nil
	}

	err := ioutil.WriteFile(c.PidFile, []byte(strconv.Itoa(c.Pid)), 0644)
	if err != nil {
		return err
	}

	return nil
}

func pipeLogs(c *Context) error {
	if !c.Logs {
		return nil
	}

	client, err := getClient(c)
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

func keepAlive(c *Context) error {
	if c.Logs || c.Rm {
		client, err := getClient(c)
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

func rmContainer(c *Context) error {
	if !c.Rm {
		return nil
	}

	client, err := getClient(c)
	if err != nil {
		return err
	}

	return client.RemoveContainer(dockerClient.RemoveContainerOptions{
		ID:    c.Id,
		Force: true,
	})
}

func mainWithArgs(args []string) (*Context, error) {
	c, err := parseContext(args)
	if err != nil {
		return c, err
	}

	err = runContainer(c)
	if err != nil {
		return c, err
	}

	_, err = moveCgroups(c)
	if err != nil {
		return c, err
	}

	err = notify(c)
	if err != nil {
		return c, err
	}

	err = pidFile(c)
	if err != nil {
		return c, err
	}

	go func() {
		_ = pipeLogs(c)
	}()

	err = keepAlive(c)
	if err != nil {
		return c, err
	}

	err = rmContainer(c)
	if err != nil {
		return c, err
	}

	return c, nil
}

func main() {
	_, err := mainWithArgs(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
}
