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
	"log"
	"math/rand"
	"os/exec"
	"regexp"
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
	if ok, err := HaveImage(image); !ok || err != nil {
		if err != nil {
			return fmt.Errorf("Error checking for docker image %s: %v", image, err)
		}
		log.Printf("Pulling docker image %s ...", image)
		if err := Pull(image); err != nil {
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

// HaveImage reports if docker have image 'name'.
func HaveImage(name string) (bool, error) {
	out, err := runDockerCommand("docker", "images", "--no-trunc").Output()
	if err != nil {
		return false, err
	}
	repo, tag := parseImageName(name)
	images := parseDockerImagesOutput(out)
	return images.contains(repo, tag), nil
}

func runService(ports []int, args ...string) (containerID string, err error) {
	var portMappings []string
	for _, port := range ports {
		forward := fmt.Sprintf(":%d", port)
		if BindDockerToLocalhost != "" {
			forward = "127.0.0.1:" + forward
		}
		portMappings = append(portMappings, "-p", forward)
	}
	return run(append(portMappings, args...)...)
}

func run(args ...string) (containerID string, err error) {
	var stdout, stderr bytes.Buffer
	validID := regexp.MustCompile(`^([a-zA-Z0-9]+)$`)
	cmd := runDockerCommand("docker", append([]string{"run"}, args...)...)

	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err = cmd.Run(); err != nil {
		err = fmt.Errorf("Error running docker\nStdOut: %s\nStdErr: %s\nError: %v\n\n", stdout.String(), stderr.String(), err)
		return
	}
	containerID = strings.TrimSpace(string(stdout.String()))
	if !validID.MatchString(containerID) {
		return "", fmt.Errorf("Error running docker: %s", containerID)
	}
	if containerID == "" {
		return "", errors.New("Unexpected empty output from `docker run`")
	}
	return containerID, nil
}

// KillContainer runs docker kill on a container.
func KillContainer(container string) error {
	if container != "" {
		return runDockerCommand("docker", "kill", container).Run()
	}
	return nil
}

// Pull retrieves the docker image with 'docker pull'.
func Pull(image string) error {
	out, err := runDockerCommand("docker", "pull", image).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v: %s", err, out)
	}
	return err
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
func Ports(containerID string) (ServicePortMap, error) {
	c, err := inspectContainer(containerID)
	if err != nil {
		return ServicePortMap{}, err
	}
	if len(c.NetworkSettings.Ports) == 0 {
		return ServicePortMap{}, errors.New("could not find any exposed ports. Not running?")
	}

	portMap := make(ServicePortMap)
	for key, x := range c.NetworkSettings.Ports {
		ports := strings.Split(key, "/")[0]
		port, err := strconv.Atoi(ports)
		if err != nil {
			return ServicePortMap{}, err
		}

		if len(x) > 0 {
			portMap[port] = x[0]
		} else {
			ip := c.NetworkSettings.IPAddress
			portMap[port] = ServicePort{Host: ip, Port: ports}
		}
	}
	return portMap, nil
}

// SetupMultiportContainer sets up a container, using the start function to run the given image.
// It also looks up the IP address of the container, and tests this address with the given
// ports and timeout. It returns the container ID and exposed services, or makes the test
// fail on error.
func SetupMultiportContainer(image string, ports []int, timeout time.Duration, start func() (string, error)) (ContainerID, ServicePorts, error) {
	err := runLongTest(image)
	if err != nil {
		return "", ServicePorts{}, err
	}

	containerID, err := start()
	if err != nil {
		return "", ServicePorts{}, err
	}

	c := ContainerID(containerID)
	svcs, err := c.lookup(ports, timeout)
	if err != nil {
		c.KillRemove()
		return "", ServicePorts{}, err
	}
	return c, svcs, nil
}

// SetupContainer sets up a container, using the start function to run the given image.
// It also looks up the IP address of the container, and tests this address with the given
// port and timeout. It returns the container ID and its Service, or makes the test
// fail on error.
func SetupContainer(image string, port int, timeout time.Duration, start func() (string, error)) (ContainerID, ServicePort, error) {
	c, svcs, err := SetupMultiportContainer(image, []int{port}, timeout, start)
	if err != nil {
		return "", ServicePort{}, err
	}
	return c, svcs.First(), nil
}

// GenerateContainerID generated a random container id.
func GenerateContainerID() string {
	return ContainerPrefix + uuid.New()
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}
