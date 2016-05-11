package dockertest

import (
	"fmt"
	"github.com/go-errors/errors"
	"log"
	"time"
)

// SetupMockserverContainer sets up a real Mockserver instance for testing purposes
// using a Docker container. It returns the exposed services
func SetupMockserverContainer() (c ContainerID, svcs ServicePorts, err error) {
	ports := []int{1080, 1090}
	c, svcs, err = SetupMultiportContainer(RabbitMQImageName, ports, 10*time.Second, func() (string, error) {
		return runService(ports, "--name", GenerateContainerID(), "-d", "-P", MockserverImageName)
	})
	return
}

// ConnectToMockserver starts a Mockserver image and passes the mock and proxy urls to the connector callback functions.
// The urls will match the http://ip:port pattern (e.g. http://123.123.123.123:4241)
func ConnectToMockserver(tries int, delay time.Duration, mockConnector func(url string) bool, proxyConnector func(url string) bool) (c ContainerID, err error) {
	c, svcs, err := SetupMockserverContainer()
	if err != nil {
		return c, fmt.Errorf("Could not set up Mockserver container: %v", err)
	}
	mockPort := svcs[0]
	proxyPort := svcs[1]

	var mockOk, proxyOk bool

	for try := 0; try <= tries; try++ {
		time.Sleep(delay)

		if !mockOk {
			if mockConnector(fmt.Sprintf("http://%s", mockPort)) {
				mockOk = true
			} else {
				log.Printf("Try %d failed for mock. Retrying.", try)
			}
		}
		if !proxyOk {
			if proxyConnector(fmt.Sprintf("http://%s", proxyPort)) {
				proxyOk = true
			} else {
				log.Printf("Try %d failed for proxy. Retrying.", try)
			}
		}
	}

	if mockOk && proxyOk {
		return c, nil
	} else {
		return c, errors.New("Could not set up Mockserver container.")
	}
}
