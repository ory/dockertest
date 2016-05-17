package dockertest

import (
	"fmt"
	"strings"
)

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

func (s Specification) WithImage(img string) Specification {
	s.Image = img
	return s
}

func rightSplit(s, delim string) (head, tail string) {
	if sep := strings.LastIndex(s, delim); sep != -1 {
		return s[:sep], s[sep+1:]
	} else {
		return s, ""
	}
}

func (s Specification) WithTag(t string) Specification {
	repo, _ := rightSplit(s.Image, ":")
	return s.WithImage(fmt.Sprintf("%s:%s", repo, t))
}

// Alias for WithTag(v)
func (s Specification) WithVersion(v string) Specification {
	return s.WithTag(v)
}

func (s Specification) WithArguments(args ...string) Specification {
	s.ImageArguments = args
	return s
}

func (s Specification) WithAddedArguments(args ...string) Specification {
	return s.WithArguments(append(s.ImageArguments, args...)...)
}
