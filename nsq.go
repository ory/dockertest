package dockertest

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// SetupNSQdContainer sets up a real NSQ instance for testing purposes
// using a Docker container and executing `/nsqd`. It returns the container ID and tcp,http services,
// or makes the test fail on error.
func SetupNSQdContainer() (c ContainerID, svcs ServicePorts, err error) {
	// --name nsqd -p 4150:4150 -p 4151:4151 nsqio/nsq /nsqd --broadcast-address=192.168.99.100 --lookupd-tcp-address=192.168.99.100:4160
	ports := []int{4150, 4151}
	c, svcs, err = SetupMultiportContainer(NSQImageName, ports, 15*time.Second, func() (string, error) {
		return runService(ports, "--name", GenerateContainerID(), "-d", "-P", NSQImageName, "/nsqd")
	})
	return
}

// SetupNSQLookupdContainer sets up a real NSQ instance for testing purposes
// using a Docker container and executing `/nsqlookupd`. It returns the container ID and tcp,http services,
// or makes the test fail on error.
func SetupNSQLookupdContainer() (c ContainerID, svcs ServicePorts, err error) {
	// docker run --name lookupd -p 4160:4160 -p 4161:4161 nsqio/nsq /nsqlookupd
	ports := []int{4160, 4161}
	c, svcs, err = SetupMultiportContainer(NSQImageName, ports, 15*time.Second, func() (string, error) {
		return runService(ports, "--name", GenerateContainerID(), "-d", "-P", NSQImageName, "/nsqlookupd")
	})
	return
}

// ConnectToNSQLookupd starts a NSQ image with `/nsqlookupd` running and passes the IP, HTTP port, and TCP port to the connector callback function.
// The url will match the ip pattern (e.g. 123.123.123.123).
func ConnectToNSQLookupd(tries int, delay time.Duration, connector func(httpPort, tcpPort ServicePort) bool) (c ContainerID, err error) {
	c, svcs, err := SetupNSQLookupdContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up NSQLookupd container: %v", err)
	}
	tcpPort := svcs[0]
	httpPort := svcs[1]

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(httpPort, tcpPort) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up NSQLookupd container.")
}

// ConnectToNSQd starts a NSQ image with `/nsqd` running and passes the IP, HTTP port, and TCP port to the connector callback function.
// The url will match the ip pattern (e.g. 123.123.123.123).
func ConnectToNSQd(tries int, delay time.Duration, connector func(httpPort, tcpPort ServicePort) bool) (c ContainerID, err error) {
	c, svcs, err := SetupNSQdContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up NSQd container: %v", err)
	}
	tcpPort := svcs[0]
	httpPort := svcs[1]

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)
		if connector(httpPort, tcpPort) {
			return c, nil
		}
		log.Printf("Try %d failed. Retrying.", try)
	}
	return c, errors.New("Could not set up NSQd container.")
}
