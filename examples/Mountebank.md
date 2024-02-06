The following is an example of using `dockertest` & `mountebank` to perform
[narrow integration testing](https://martinfowler.com/bliki/IntegrationTest.html).

# Go Code

```go
package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	log "github.com/sirupsen/logrus"
)

var imposterPort string

func TestMain(m *testing.M) {
	pwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to get working directory: %s", err)
	}

	// sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository:   "jkris/mountebank",
		Tag:          "2.4.0",
		// Expose both the default Mountebank port and that of our imposter, defined in imposter.json
		ExposedPorts: []string{"2525", "8090"},
		Cmd:          []string{"--configfile", "imposter.json"},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.Mounts = []docker.HostMount{
			{
				Target: "/imposter.json",
				Source: fmt.Sprintf("%s/imposter.json", pwd),
				Type:   "bind",
			},
		}
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	// Get resource's published port for our imposter.
	imposterPort = resource.GetPort("8090/tcp")

	client := http.Client{}

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet.
	if err = pool.Retry(func() error {
		// Hitting the default mountebank URL to see if the container is usable yet.
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s/imposters", resource.GetPort("2525/tcp")), nil)
		if err != nil {
			return err
		}

		_, err = client.Do(req)

		return err
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	defer func() {
		if err := pool.Purge(resource); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
    }()

	// run tests
	m.Run()
}

func TestHandler(t *testing.T) {
	tests := []struct {
		name             string
		givenRequestURL  string
		givenMethod      string
		expectedStatus   int
	}{
		{
			name:             "given valid request, expect 200",
			givenRequestURL:  "/test",
			givenMethod:      http.MethodGet,
			expectedStatus:   http.StatusOK,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			imposterBaseURL := fmt.Sprintf("http://localhost:%s", imposterPort)

			h := handler.New(
				controller.New(
					externalService.New(imposterBaseURL),
				),
			)

			rr := httptest.NewRecorder()

			req := httptest.NewRequest(test.givenMethod, test.givenRequestURL, nil)

			router := new(mux.Router)
			router.HandleFunc(test.givenURL, h.Get)
			router.ServeHTTP(rr, req)

			res := rr.Result()

			if !cmp.Equal(res.StatusCode, test.expectedStatus) {
				t.Fatal(cmp.Diff(res.StatusCode, test.expectedStatus))
			}
		})
	}
}

```

# Basic Imposter

Below is the content of `imposter.json`

```json
{
  "port": 8090,
  "protocol": "http",
  "stubs": [
    {
      "responses": [{ "is": { "statusCode": 200 } }],
      "predicates": [
        {
          "equals": {
            "path": "/test",
            "method": "GET",
            "headers": { "Content-Type": "application/json" }
          }
        }
      ]
    }
  ]
}
```
