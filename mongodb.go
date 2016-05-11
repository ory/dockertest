package dockertest

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// SetupMongoContainer sets up a real MongoDB instance for testing purposes,
// using a Docker container. It returns the container ID and its Service,
// or makes the test fail on error.
func SetupMongoContainer() (c ContainerID, svc ServicePort, err error) {
	forward := fmt.Sprintf(":%d", 27017)
	if BindDockerToLocalhost != "" {
		forward = "127.0.0.1:" + forward
	}
	c, svc, err = SetupContainer(MongoDBImageName, 27017, 10*time.Second, func() (string, error) {
		res, err := runService([]int{27017}, "--name", GenerateContainerID(), "-d", "-P", MongoDBImageName)
		return res, err
	})
	return
}

// ConnectToMongoDB starts a MongoDB image and passes the database url to the connector callback.
// The url will match the ip:port pattern (e.g. 123.123.123.123:4241)
func ConnectToMongoDB(tries int, delay time.Duration, connector func(url string) bool) (c ContainerID, err error) {
	c, svc, err := SetupMongoContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up MongoDB container: %v", err)
	}

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(svc.String()) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up MongoDB container.")
}
