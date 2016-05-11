package dockertest

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// SetupElasticSearchContainer sets up a real ElasticSearch instance for testing purposes
// using a Docker container. It returns the container ID and its Service,
// or makes the test fail on error.
func SetupElasticSearchContainer() (c ContainerID, svc ServicePort, err error) {
	c, svc, err = SetupContainer(ElasticSearchImageName, 9200, 15*time.Second, func() (string, error) {
		return runService([]int{9200}, "--name", GenerateContainerID(), "-d", "-P", ElasticSearchImageName)
	})
	return
}

// ConnectToElasticSearch starts an ElasticSearch image and passes the database url to the connector callback function.
// The url will match the ip:port pattern (e.g. 123.123.123.123:4241)
func ConnectToElasticSearch(tries int, delay time.Duration, connector func(url string) bool) (c ContainerID, err error) {
	c, svc, err := SetupElasticSearchContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up ElasticSearch container: %v", err)
	}

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(svc.String()) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up ElasticSearch container.")
}
