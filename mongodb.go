package dockertest

import (
	"errors"
	"fmt"
	"log"
	"time"
)

const mongoDBPort = 27017

// SetupMongoContainer sets up a real MongoDB instance for testing purposes,
// using a Docker container. It returns the container ID and its IP address,
// or makes the test fail on error.
func SetupMongoContainer() (c ContainerID, ip string, port int, err error) {
	port = RandomPort()
	forward := fmt.Sprintf("%d:%d", port, mongoDBPort)
	if BindDockerToLocalhost != "" {
		forward = "127.0.0.1:" + forward
	}
	c, ip, err = SetupContainer(MongoDBImageName, mongoDBPort, 10*time.Second, func() (string, error) {
		return run("--name", GenerateContainerID(), "-d", "-P", "-p", forward, MongoDBImageName)
	})
	return
}

// ConnectToMongoDB starts a MongoDB image and passes the database url to the connector callback.
// The url will match the ip:port pattern (e.g. 123.123.123.123:4241)
func ConnectToMongoDB(tries int, delay time.Duration, connector func(url string) bool) (c ContainerID, err error) {
	c, ip, _, err := SetupMongoContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up MongoDB container: %v", err)
	}

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		url := fmt.Sprintf("%s:%d", ip, mongoDBPort)
		if connector(url) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up MongoDB container.")
}
