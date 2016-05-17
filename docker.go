package dockertest

/*
Copyright 2014 The Camlistore Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"time"

	// Import postgres driver
	_ "github.com/lib/pq"
	"github.com/pborman/uuid"
)

/// runLongTest checks all the conditions for running a docker container
// based on image.
func runLongTest(image string) error {
	DockerMachineAvailable = false
	if haveDockerMachine() {
		DockerMachineAvailable = true
		if !startDockerMachine() {
			log.Printf(`Starting docker machine "%s" failed.
This could be because the image is already running or because the image does not exist.
Tests will fail if the image does not exist.`, DockerMachineName)
		}
	} else if !haveDocker() {
		return errors.New("Neither 'docker' nor 'docker-machine' available on this system.")
	}
	if ok, err := haveImage(image); !ok || err != nil {
		if err != nil {
			return fmt.Errorf("Error checking for docker image %s: %v", image, err)
		}
		log.Printf("Pulling docker image %s ...", image)
		if err := pull(image); err != nil {
			return fmt.Errorf("Error pulling %s: %v", image, err)
		}
	}
	return nil
}

func runDockerCommand(command string, args ...string) *exec.Cmd {
	if DockerMachineAvailable {
		command = "/usr/local/bin/" + strings.Join(append([]string{command}, args...), " ")
		cmd := exec.Command("docker-machine", "ssh", DockerMachineName, command)
		return cmd
	}
	return exec.Command(command, args...)
}

// haveDockerMachine returns whether the "docker" command was found.
func haveDockerMachine() bool {
	_, err := exec.LookPath("docker-machine")
	return err == nil
}

// startDockerMachine starts the docker machine and returns false if the command failed to execute
func startDockerMachine() bool {
	_, err := exec.Command("docker-machine", "start", DockerMachineName).Output()
	return err == nil
}

// haveDocker returns whether the "docker" command was found.
func haveDocker() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

type dockerImage struct {
	repo string
	tag  string
}

type dockerImageList []dockerImage

func (l dockerImageList) contains(repo string, tag string) bool {
	if tag == "" {
		tag = "latest"
	}
	for _, image := range l {
		if image.repo == repo && image.tag == tag {
			return true
		}
	}
	return false
}

func parseDockerImagesOutput(data []byte) (images dockerImageList) {
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return
	}

	// skip first line with columns names
	images = make(dockerImageList, 0, len(lines)-1)
	for _, line := range lines[1:] {
		cols := strings.Fields(line)
		if len(cols) < 2 {
			continue
		}

		image := dockerImage{
			repo: cols[0],
			tag:  cols[1],
		}
		images = append(images, image)
	}

	return
}

func parseImageName(name string) (repo string, tag string) {
	if fields := strings.SplitN(name, ":", 2); len(fields) == 2 {
		repo, tag = fields[0], fields[1]
	} else {
		repo = name
	}
	return
}

// haveImage reports if docker have image 'name'.
func haveImage(name string) (bool, error) {
	out, err := runDockerCommand("docker", "images", "--no-trunc").Output()
	if err != nil {
		return false, err
	}
	repo, tag := parseImageName(name)
	images := parseDockerImagesOutput(out)
	return images.contains(repo, tag), nil
}

type Container interface {
	Destroy() error

	// "Main" service URL in service-specific format
	ServiceURL() string

	// Should at least contain "main". May contain extra
	URLs() ServiceURLMap

	Log() io.Reader
}

type dockerContainer struct {
	docker *dockerRunner
	id     string
	log    *logBuffer
	urls   ServiceURLMap
}

func (c dockerContainer) Destroy() error {
	return c.docker.Destroy(c.id)
}

func (c dockerContainer) ServiceURL() string {
	return c.URLs()["main"]
}

func (c dockerContainer) URLs() ServiceURLMap {
	return c.urls
}

func (c dockerContainer) Log() io.Reader {
	return c.log.Reader()
}

type dockerRunner struct{}

func (dockerRunner) Destroy(containerId string) error {
	return runDockerCommand("docker", "rm", "-f", containerId).Run()
}

func (r dockerRunner) Deploy(spec Specification) (c Container, err error) {
	if err := runLongTest(spec.Image); err != nil {
		return nil, err
	}

	id := generateContainerID()
	args := []string{"run", "--name", id, "-P"}
	args = append(args, portMappingArguments(spec.Services.PublishedPorts())...)
	for k, v := range spec.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, spec.Image)
	args = append(args, spec.ImageArguments...)

	l := newLog()
	cmd := runDockerCommand("docker", args...)
	cmd.Stdout = l
	cmd.Stderr = l
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Docker command failed: %s", err)
	}
	go func() {
		cmd.Wait()
		l.Close()
	}()
	defer func() {
		// Dying without returning the container
		if c == nil {
			r.Destroy(id)
		}
	}()

	// Wait for first output
	if _, err := l.Reader().Read([]byte{}); err != nil {
		return nil, fmt.Errorf("Failed to read any output from command: %s", err)
	}

	ports, err := ports(id)
	if err != nil {
		return nil, fmt.Errorf("Failed to read ports from container: %s", err)
	}
	services, err := spec.Services.Map(ports)
	if err != nil {
		return nil, fmt.Errorf("Failed to map ports into service URLs: %v", err)
	}

	c_ := dockerContainer{
		docker: &r,
		id:     id,
		log:    l,
		urls:   services,
	}
	if err := spec.Waiter.WaitForReady(c_); err != nil {
		return nil, fmt.Errorf("Failed to get ready-signal from container: %s", err)
	}

	if err := ports.Wait(time.Second * 5); err != nil {
		return nil, fmt.Errorf("Failed to connect to published ports")
	}

	return c_, nil
}

func portMappingArguments(ports []int) []string {
	var portMappings []string
	for _, port := range ports {
		forward := fmt.Sprintf(":%d", port)
		if BindDockerToLocalhost {
			forward = "127.0.0.1:" + forward
		}
		portMappings = append(portMappings, "-p", forward)
	}
	return portMappings
}

// pull retrieves the docker image with 'docker pull'.
func pull(image string) error {
	out, err := runDockerCommand("docker", "pull", image).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v: %s", err, out)
	}
	return err
}

// internal structure used for describing a running container
type container struct {
	NetworkSettings struct {
		IPAddress string
		Ports     map[string][]PublicPort
	}
}

func inspectContainer(containerID string) (container, error) {
	out, err := runDockerCommand("docker", "inspect", containerID).Output()
	if err != nil {
		return container{}, err
	}
	var c []container
	if err := json.NewDecoder(bytes.NewReader(out)).Decode(&c); err != nil {
		return container{}, err
	}
	if len(c) == 0 {
		return container{}, errors.New("no output from docker inspect")
	}
	return c[0], nil
}

// Return the exposed Services, on their bound public port
func ports(containerID string) (PortMap, error) {
	c, err := inspectContainer(containerID)
	if err != nil {
		return PortMap{}, err
	}
	if len(c.NetworkSettings.Ports) == 0 {
		return PortMap{}, errors.New("could not find any exposed ports. Not running?")
	}

	var hostIp = "127.0.0.1"
	if DockerMachineAvailable {
		b, err := exec.Command("docker-machine", "ip", DockerMachineName).Output()
		if err != nil {
			return nil, fmt.Errorf("Failed to get docker-machine ip: %s", err)
		}
		hostIp = strings.TrimSpace(string(b))
	}

	portMap := make(PortMap)
	for key, x := range c.NetworkSettings.Ports {
		ports := strings.Split(key, "/")[0]
		port, err := strconv.Atoi(ports)
		if err != nil {
			return PortMap{}, err
		}

		if len(x) > 0 {
			if x[0].Host == "0.0.0.0" {
				x[0].Host = hostIp
			}
			portMap[port] = x[0]
		} else {
			ip := c.NetworkSettings.IPAddress
			portMap[port] = PublicPort{Host: ip, Port: ports}
		}

	}
	return portMap, nil
}

// generateContainerID generated a random container id.
func generateContainerID() string {
	return ContainerPrefix + uuid.New()
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}
