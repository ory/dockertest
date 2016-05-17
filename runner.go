package dockertest

type Runner interface {
	Deploy(spec Specification) (Container, error)
}

var DefaultRunner Runner = dockerRunner{}

func Deploy(spec Specification) (Container, error) {
	return DefaultRunner.Deploy(spec)
}
