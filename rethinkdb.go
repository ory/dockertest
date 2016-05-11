package dockertest

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// SetupRethinkDBContainer sets up a real RethinkDB instance for testing purposes,
// using a Docker container. It returns the container ID and its Service,
// or makes the test fail on error.
func SetupRethinkDBContainer() (c ContainerID, svc ServicePort, err error) {
	c, svc, err = SetupContainer(RethinkDBImageName, 28015, 10*time.Second, func() (string, error) {
		return runService([]int{28015}, "--name", GenerateContainerID(), "-d", "-P", RethinkDBImageName)
	})
	return
}

// ConnectToRethinkDB starts a RethinkDB image and passes the database url to the connector callback.
// The url will match the ip:port pattern (e.g. 123.123.123.123:4241)
func ConnectToRethinkDB(tries int, delay time.Duration, connector func(url string) bool) (c ContainerID, err error) {
	c, svc, err := SetupRethinkDBContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up RethinkDB container: %v", err)
	}

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(svc.String()) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up RethinkDB container.")
}
