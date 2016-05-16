package dockertest

type Specification struct {
	// Name of the docker-image to launch from
	Image string
	// List of arguments to pass to the running docker-image. I.E. after image-
	// name in "docker run"
	ImageArguments []string
	// Environment passed to the Image. Useful for things like configuring
	// credentials to connect to services
	Env Env

	// A ReadyWaiter-implementation. Normally a RegexWaiter should suffice
	Waiter ReadyWaiter
	// A ServiceMap-implementation. Normally a SimpleServiceMap will suffice
	Services ServiceMap
}

type Env map[string]string

type ReadyWaiter interface {
	// Inspect Container and wait for it to be ready
	WaitForReady(Container) error
}

type ServiceURLMap map[string]string
type ServiceMap interface {
	PublishedPorts() []int
	Map(ServicePortMap) (ServiceURLMap, error)
}
