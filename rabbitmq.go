package dockertest

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// SetupRabbitMQContainer sets up a real RabbitMQ instance for testing purposes,
// using a Docker container. It returns the container ID and its Service,
// or makes the test fail on error.
func SetupRabbitMQContainer() (c ContainerID, svc ServicePort, err error) {
	c, svc, err = SetupContainer(RabbitMQImageName, 5672, 10*time.Second, func() (string, error) {
		res, err := runService([]int{5672}, "--name", GenerateContainerID(), "-d", "-P", RabbitMQImageName)
		return res, err
	})
	return
}

// ConnectToRabbitMQ starts a RabbitMQ image and passes the amqp url to the connector callback.
// The url will match the ip:port pattern (e.g. 123.123.123.123:4241)
func ConnectToRabbitMQ(tries int, delay time.Duration, connector func(url string) bool) (c ContainerID, err error) {
	c, svc, err := SetupRabbitMQContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up RabbitMQ container: %v", err)
	}

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(svc.String()) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up RabbitMQ container.")
}
