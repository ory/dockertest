package dockertest

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// SetupRedisContainer sets up a real Redis instance for testing purposes
// using a Docker container. It returns the container ID and its Service,
// or makes the test fail on error.
func SetupRedisContainer() (c ContainerID, svc ServicePort, err error) {
	c, svc, err = SetupContainer(RedisImageName, 6379, 15*time.Second, func() (string, error) {
		return runService([]int{6379}, "--name", GenerateContainerID(), "-d", "-P", RedisImageName)
	})
	return
}

// ConnectToRedis starts a Redis image and passes the database url to the connector callback function.
// The url will match the ip:port pattern (e.g. 123.123.123.123:6379)
func ConnectToRedis(tries int, delay time.Duration, connector func(url string) bool) (c ContainerID, err error) {
	c, svc, err := SetupRedisContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up Redis container: %v", err)
	}

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(svc.String()) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up Redis container.")
}
