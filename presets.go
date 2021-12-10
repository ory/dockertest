package dockertest

// RunOption is a functional option that manipulates the default RunOptions.
type RunOption func(*RunOptions)

// WithTag allows for clients to define the tag they wish.
func WithTag(tag string) RunOption {
	return func(options *RunOptions) {
		options.Tag = tag
	}
}

// WithEnv allows for clients to define the environment variables they wish.
func WithEnv(env []string) RunOption {
	return func(options *RunOptions) {
		options.Env = env
	}
}

// WithExposedPorts allows for clients to define the exposed ports.
func WithExposedPorts(ports []string) RunOption {
	return func(options *RunOptions) {
		options.ExposedPorts = ports
	}
}

// WithCMD allows for clients to define the command they wish.
func WithCMD(cmd []string) RunOption {
	return func(options *RunOptions) {
		options.Cmd = cmd
	}
}

// Postgres returns a default RunOptions config for postgres with ability to overwrite the values.
func Postgres(opts ...RunOption) *RunOptions {
	r := &RunOptions{
		Repository: "postgres",
		Tag:        "latest",
		Env: []string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_USER=user_name",
			"POSTGRES_DB=dbname",
			"listen_addresses='*'",
		},
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Cassandra returns a default RunOptions config for cassandra with ability to overwrite the values.
func Cassandra(opts ...RunOption) *RunOptions {
	r := &RunOptions{
		Repository: "cassandra",
		Tag:        "latest",
		Mounts:     []string{"/tmp/local-cassandra:/etc/cassandra"},
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// MySQL returns a default RunOptions config for MySQL with ability to overwrite the values.
func MySQL(opts ...RunOption) *RunOptions {
	r := &RunOptions{
		Repository: "mysql",
		Tag:        "latest",
		Env: []string{
			"MYSQL_ROOT_PASSWORD=secret",
		},
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Redis returns a default RunOptions config for redis with ability to overwrite the values.
func Redis(opts ...RunOption) *RunOptions {
	r := &RunOptions{
		Repository: "bitnami/redis",
		Tag:        "latest",
		Env: []string{
			"ALLOW_EMPTY_PASSWORD=yes",
		},
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}
