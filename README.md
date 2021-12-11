# Introduction
This repository provides a wrapper which improves the handling of Docker containers run as `systemd` services. 

If a Docker container is started as a `systemd` service using the "usual" `docker run ...` instruction, f.ex. 
`ExecStart=docker run ...`, **`systemd` interacts with the Docker client process instead of the container 
process, which can lead to situations where `systemd`'s capacity to monitor process health is affected**:
- the client can detach or crash while the container is doing fine, yet `systemd` would trigger failure handling 
- worse, the container crashes and should be taken care of, but the client stalled - `systemd` is blind and won't do  
  anything
- when a container is stopped with `docker stop ...`, attached client processes exit with error code 143, not 
  0/success, which triggers `systemd`'s failure handling unless it's explicitely configured to ignore this using 
  `SuccessExitStatus=143`, but that's a workaround. The problem is well explained in
  [this issue description](https://github.com/jenkinsci/docker/issues/485)

The **key thing that this wrapper does is** that it moves the container process from the *cgroups set up by Docker* 
to the *service unit's cgroup* **to give `systemd` the supervision of the actual Docker container process**.  
It's written in Golang and allows to *leverage all the cgroup functionality of `systemd` and `systemd-notify`*.

# Repository history and credits
- the code was written by [@ibuildthecloud](https://github.com/ibuildthecloud) and his co-contributors in this [repository](https://github.com/ibuildthecloud/systemd-docker). 
The motivation is explained in this [Docker issue #6791](https://github.com/docker/docker/issues/6791) and this [mailing list thread](https://groups.google.com/d/topic/coreos-dev/wf7G6rA7Bf4/discussion).
- [@agend07](https://github.com/agend07/systemd-docker) and co-contributors fixed outdated dependancies and did a first clean-up
- [@dontsetse](https://github.com/dontsetse/systemd-docker) made a bunch of improvements in 2018
- [@embtom](https://github.com/embtom/systemd-docker) removed all outdated and broken elements and created a new compilation docker container
- various developers have contributed numerous improvements
- I have refactored the project into smaller files and added health check to sd_notify integration 

# Installation
Supposing that a Go environment is available, the build instruction is `go get github.com/DonTseTse/systemd-docker`. The 
executable can then be found in the Go binary directory (usually something like `$GO_ROOT/bin`) and it's called 
`systemd-docker`.

It can also be build using a stand-alone docker image, see [here](https://github.com/DonTseTse/systemd-docker_build-container)

# Use
Both
- `systemctl` to manage `systemd` services, and
- the `docker` CLI

can be used and everything should stay in sync.

In the `systemd` unit files, the instruction to launch the Docker container takes the form 

`ExecStart=/path/to/systemd-docker [<systemd-docker_options>] -- <docker-run_parameters>`

where
- `/path/to/systemd-docker` is the absolute path of the `systemd-docker` executable
- `<systemd-docker_options>` are the [flags to configure systemd-docker](#systemd-docker-options)
- `<docker-run_parameters>` are forwarded to `docker run`. A few restrictions apply, see section 
  [Docker run restrictions](#docker-restrictions)

The example below shows a typical `systemd` unit file using `systemd-docker` (supposed to be in `/usr/bin`), running a 
Nginx container:
```ini
[Unit]
Description=Nginx
After=docker.service
Requires=docker.service

[Service]
#--- if systemd-notify is used
Type=notify
NotifyAccess=all
#------------------------
ExecStart=/usr/bin/systemd-docker -- --rm --name %n nginx
Restart=always
RestartSec=10s
TimeoutStartSec=120
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
```
The use of `%n` is a `systemd` feature explained [here](#automatic-container-naming). Supposing that the unit file example 
given above is stored under the likely path `/etc/systemd/system/nginx.service`, the container is named *nginx*. 
 
For the details about `Type=notify` and `NotifyAccess=all` and `systemd-notify`, see 
[systemd notifications](#systemd-notifications). 

For a general documentation of all `systemd` unit file configurations
options, see this [documentation](https://www.freedesktop.org/software/systemd/man/systemd.service.html).

## Container names
Container names are compulsory to make sure that each `systemd` service always relates to/acts upon the same container(s). 
While it may seem as if that could be omitted as long as the `--rm` flag is used to make Docker remove any stopped 
container, that's misleading: the deletion process triggered by this flag is actually part of the Docker client logic and 
if the client detaches for whatever reason from the running container, the information is lost (even if another client is
re-attached later) and *the container will **not** be deleted* upon termination. `systemd-docker` adds an additional check 
and looks for the named container when `systemd-docker ... -- ...` is called - if a stopped container exists, it's removed.

# Systemd integration details
## Automatic container naming
While it processes unit files, `systemd` populates a range of variables among which `%n` stands for the name of service, 
derived from it's filename. This  allows to write a self-configuring `ExecStart` instruction using the parameters
 
`ExecStart=/path/to/systemd-docker ... -- ... --name %n --rm ...`

## Use of systemd environment variables
`systemd` handles environment variables with the instructions `Environment=...` and `EnvironmentFile=...`. To inject
variables into other instructions, the pattern is *${variable_name}*. With the `docker run` flag `-e` they can be passed 
from `systemd` to the Docker container

Example: `ExecStart=/path/to/systemd-docker ... -- -e ABC=${ABC} -e XYZ=${XYZ} ...`

`systemd-docker` has an option to pass on all defined environment variables using the `--env` flag, explained 
[here](#environment-variables)

## Systemd notifications (systemd-notify)
`systemd-notify` can be used to schedule and sequence the launch of different services. The `systemd` 
[documentation](https://www.freedesktop.org/software/systemd/man/systemd.service.html) explains the configuration optionss 
available in unit files: 
- `Type=notify`: "... it is expected that the daemon sends a notification message via sd_notify(3) or an equivalent call when 
  it has finished starting up. systemd will proceed with starting follow-up units after this notification message has been 
  sent."
- `NotifyAccess=all`: "Controls access to the service status notification socket, as accessible via the sd_notify(3) call. ...
  If all, all services updates from all members of the service's control group are accepted."

By default `systemd-docker` do either:
1. If a health check in enabled, `systemd-docker` monitor the results and send READY=1 to the `systemd` notification 
socket once the service is healthy.  `systemd-docker` will also send WATCHDOG=1 to the `systemd` notification socket 
for every health check success.
2. Send READY=1 to the `systemd` notification socket.

Alternatively, notifying systemd can be delegated to the container.
 
See [systemd-notify support](#systemd-notify-support) for more details.

Please be aware that `systemd-notify` comes with its own quirks - more info can be found in this
[mailing list thread](http://comments.gmane.org/gmane.comp.sysutils.systemd.devel/18649).  In short, `systemd-notify` 
is not reliable because often the child dies before `systemd` has time to determine which cgroup it is a member of.

# Systemd-docker options
## Logging
By default the container's stdout/stderr is written to the system journal. This may be disabled with `--logs=false`.

Example: `ExecStart=/path/to/systemd-docker ... --logs=false ... -- ...`

## Environment Variables
The `systemd` environment variables are automatically passed through to the Docker container if the `--env` flag is set.  
It will essentially read all the current environment variables and add the appropriate `-e ...` flags to the 
`docker run` command.

```
EnvironmentFile=/etc/environment
ExecStart=systemd-docker ... --env ... -- ...
```
In the example above, all environment variables defined in `/etc/environment` will be passed to the `docker run` command.

## PID File
To create a PID file for the container, use the flag `--pid-file=</path/to/pid_file>`.

Example: `ExecStart=/path/to/systemd-docker ... --pid-file=/var/run/%n.pid ... -- ...`

## systemd-notify support

By default `systemd-docker` will inspect the container for a health check and will use the health check results to 
determine when to send `systemd-notify READY=1` and `systemd-notify WATCHDOG=1`.  If there is no health check, then 
`systemd-docker` will send `systemd-notify READY=1` once the container has started.

The `systemd-docker` flag `--notify` makes `systemd-docker` delegate the `systemd-notify READY=1` call to the container 
itself. To allow the container to achieve this, `systemd-docker` bind mounts the `systemd` notification socket into the 
container and sets the NOTIFY_SOCKET environment variable. 

Example: `ExecStart=/path/to/systemd-docker ... --notify ... -- ...`

## Container removal behavior

To disable `systemd-docker`'s "remove stopped container" procedure, the flag `... --rm=false ...` can be used.

Example: `ExecStart=/path/to/systemd-docker ... --rm=false ... -- ...`

## Additional networks

`systemd-docker` can join the container to additional networks when the container is started by including 
the `... --networks=<NETWORK> ... flag.

Examples:

1. `ExecStart=/path/to/systemd-docker ... --networks=network_name ... -- ...`
2. `ExecStart=/path/to/systemd-docker ... --networks=network_name:192.168.1.123 ... -- ...`
3. `ExecStart=/path/to/systemd-docker ... --networks=network_name,other_network_name ... -- ...`
4. `ExecStart=/path/to/systemd-docker ... --networks=network_name:192.168.1.123,other_network_name:192.168.2.123 ... -- ...`

# Docker restrictions
## --cpuset and/or -m
These flags can't be used because they are incompatible with the cgroup migration(s) inherent to `systemd-docker`. 

## -d (detaching the Docker client)
The `-d` flag provided to `docker run` has no effect under `systemd-docker`. To cause the Docker client to detach after 
the container is running, use the `systemd-docker` options `--rm=false`. If `--rm` is true, the Docker client instance 
used by `systemd-docker` is kept alive until the `systemd` service is stopped or the container exits.


# License
See [repository history and credits](#repository-history-and-credits) for acknowledgments. The work on this repository 
was done in 2021 by kadaan. 

Licensed under the [Apache License, Version 2.0](LICENSE)
