package dockertest

import "github.com/ory-am/common/env"

// Dockertest configuration
var (
	// Debug if set, prevents any container from being removed.
	Debug bool

	// DockerMachineAvailable if true, uses docker-machine to run docker commands (for running tests on Windows and Mac OS)
	DockerMachineAvailable bool

	// DockerMachineName is the machine's name. You might want to use a dedicated machine for running your tests.
	// You can set this variable either directly or by defining a DOCKERTEST_IMAGE_NAME env variable.
	DockerMachineName = env.Getenv("DOCKERTEST_IMAGE_NAME", "default")

	// BindDockerToLocalhost if set, forces docker to bind the image to localhost. This for example is required when running tests on travis-ci.
	// You can set this variable either directly or by defining a DOCKERTEST_BIND_LOCALHOST env variable.
	// FIXME DOCKER_BIND_LOCALHOST remove legacy support
	BindDockerToLocalhost = len(env.Getenv("DOCKERTEST_BIND_LOCALHOST", env.Getenv("DOCKER_BIND_LOCALHOST", ""))) > 0

	// ContainerPrefix will be prepended to all containers started by dockertest to make identification of these "test images" hassle-free.
	ContainerPrefix = env.Getenv("DOCKERTEST_CONTAINER_PREFIX", "dockertest-")
)
