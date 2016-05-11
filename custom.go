package dockertest

import (
	"errors"
	"log"
	"time"
)

// SetupCustomContainer sets up a real an instance of the given image for testing purposes,
// using a Docker container. It returns the container ID and its Service,
// or makes the test fail on error.
func SetupCustomContainer(imageName string, exposedPort int, timeOut time.Duration, extraDockerArgs ...string) (c ContainerID, svc ServicePort, err error) {
	c, svc, err = SetupContainer(imageName, exposedPort, timeOut, func() (string, error) {
		args := make([]string, 0, len(extraDockerArgs)+7)
		args = append(args, "--name", GenerateContainerID(), "-d", "-P")
		args = append(args, extraDockerArgs...)
		args = append(args, imageName)
		return runService([]int{exposedPort}, args...)
	})
	return
}

// ConnectToCustomContainer attempts to connect to a custom container until successful or the maximum number of tries is reached.
func ConnectToCustomContainer(url string, tries int, delay time.Duration, connector func(url string) bool) error {
	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(url) {
			return nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return errors.New("Could not set up custom container.")
}
