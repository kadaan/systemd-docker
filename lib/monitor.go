package lib

import (
	"errors"
	"github.com/fsouza/go-dockerclient"
	"net"
	"strings"
)

type Monitor interface {
	Close() error
	Start(conn net.Conn) error
}

type monitor struct {
	context            *Context
	client             *docker.Client
	listener           chan *docker.APIEvents
	healthCheckCommand string
}

func CreateMonitor(c *Context) (Monitor, error) {
	client, err := c.GetClient()
	if err != nil {
		return nil, err
	}

	containerOptions := docker.InspectContainerOptions{ID: c.Id}
	container, err := client.InspectContainerWithOptions(containerOptions)
	if err != nil {
		return nil, err
	}

	if container.Config.Healthcheck == nil || container.Config.Healthcheck.Test == nil || len(container.Config.Healthcheck.Test) == 0 {
		c.Log.Infof("Container '%s' does not have health check, skipping monitor creation\n", c.Id)
		return nil, nil
	}

	var healthCheckTests []string
	for i := range container.Config.Healthcheck.Test {
		if i > 0 || (i == 0 && container.Config.Healthcheck.Test[i] != "CMD" && container.Config.Healthcheck.Test[i] != "CMD-SHELL") {
			healthCheckTests = append(healthCheckTests, container.Config.Healthcheck.Test[i])
		}
	}

	healthCheckCommand := strings.Join(healthCheckTests, " ")
	c.Log.Infof("Creating health check monitor for container '%s', watching health check: %s\n", c.Id, healthCheckCommand)

	listener := make(chan *docker.APIEvents)
	eventsOptions := docker.EventsOptions{
		Filters: map[string][]string{
			"type": {"container"},
			"container": {c.Id},
			"event": {"health_status", "exec_start", "exec_die", "stop", "kill", "die", "oom"},
		},
	}

	if err = client.AddEventListenerWithOptions(eventsOptions, listener); err != nil {
		return nil, err
	}

	return &monitor{
		context: c,
		client: client,
		listener: listener,
		healthCheckCommand: healthCheckCommand,
	}, nil
}


func (m *monitor) Start(conn net.Conn) error {
	m.context.Log.Infof("Starting health check monitor for container '%s'\n", m.context.Id)
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)
	ready := false
	lastHealthCheckCommandExecuteId := ""
	for {
		select {
		case ev, ok := <-m.listener:
			if !ok || ev == nil {
				return errors.New("event listener closed")
			}
			if strings.HasPrefix(ev.Action, "health_status: ") {
				if ev.Action == "health_status: healthy" {
					ready = m.notify(conn, ready)
				}
			} else if ev.Action == "stop" || ev.Action == "kill" || ev.Action == "die" || ev.Action == "oom" {
				m.context.Log.Infof("Container '%s' is exiting, stopping health check monitor\n", m.context.Id)
				return nil
			} else if strings.HasPrefix(ev.Action, "exec_start: ") {
				if strings.HasSuffix(ev.Action, m.healthCheckCommand) {
					lastHealthCheckCommandExecuteId = ev.Actor.Attributes["execID"]
				}
			} else if ev.Action == "exec_die" {
				if ev.Actor.Attributes["execID"] == lastHealthCheckCommandExecuteId {
					if ev.Actor.Attributes["exitCode"] == "0" {
						ready = m.notify(conn, ready)
					} else {
						m.context.Log.Debugf("Container '%s' health check '%s' failed with exitCode '%s'.  Skipping notify.\n", m.context.Id, lastHealthCheckCommandExecuteId, ev.Actor.Attributes["exitCode"])
					}
				}
			}
		}
	}
}

func (m *monitor) notify(conn net.Conn, ready bool) bool {
	if !ready {
		if _, err := conn.Write([]byte("READY=1")); err == nil {
			m.context.Log.Infof("Signaled to systemd that the container '%s' is healthy\n", m.context.Id)
		} else {
			m.context.Log.Errorf("Failed to signal to systemd that the container '%s' is healthy: %s\n", m.context.Id, err)
			return false
		}
	} else {
		if _, err := conn.Write([]byte("WATCHDOG=1")); err == nil {
			m.context.Log.Debugf("Signaled to systemd watchdog that the container '%s' is still healthy\n", m.context.Id)
		} else {
			m.context.Log.Errorf("Failed to signal to systemd watchdog that the container '%s' is still healthy: %s\n", m.context.Id, err)
		}
	}
	return true
}

func (m *monitor) Close() error {
	m.context.Log.Infof("Closing health check monitor for container '%s'\n", m.context.Id)
	if err := m.client.RemoveEventListener(m.listener); err != nil {
		return err
	}
	return nil
}
