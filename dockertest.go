package dockertest

import (
	"fmt"
	"github.com/cenk/backoff"
	dc "github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
	"time"
)

type Pool struct {
	Client  *dc.Client
	MaxWait time.Duration
}

type Resource struct {
	Container *dc.Container
}

func (r *Resource) GetPort(id string) string {
	if r.Container == nil {
		return ""
	} else if r.Container.NetworkSettings == nil {
		return ""
	}

	m, ok := r.Container.NetworkSettings.Ports[dc.Port(id)]
	if !ok {
		return ""
	} else if len(m) == 0 {
		return ""
	}

	return m[0].HostPort
}

func NewPool(endpoint string) (*Pool, error) {
	if endpoint == "" {
		endpoint = "http://localhost:2375"
	}
	client, err := dc.NewClient(endpoint)
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	return &Pool{
		Client: client,
	}, nil
}

func (d *Pool) Run(repository, tag string, env []string) (*Resource, error) {
	if tag == "" {
		tag = "latest"
	}

	if err := d.Client.PullImage(dc.PullImageOptions{
		Repository: repository,
		Tag:        tag,
	}, dc.AuthConfiguration{}); err != nil {
		return nil, errors.Wrap(err, "")
	}

	c, err := d.Client.CreateContainer(dc.CreateContainerOptions{
		Config: &dc.Config{
			Image: fmt.Sprintf("%s:%s", repository, tag),
			Env:   env,
		},
		HostConfig: &dc.HostConfig{
			PublishAllPorts: true,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	if err := d.Client.StartContainer(c.ID, nil); err != nil {
		return nil, errors.Wrap(err, "")
	}

	c, err = d.Client.InspectContainer(c.ID)
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	return &Resource{
		Container: c,
	}, nil
}

func (d *Pool) Purge(r *Resource) error {
	if err := d.Client.KillContainer(dc.KillContainerOptions{ID: r.Container.ID}); err != nil {
		return errors.Wrap(err, "")
	}

	if err := d.Client.RemoveContainer(dc.RemoveContainerOptions{ID: r.Container.ID, Force: true, RemoveVolumes: true}); err != nil {
		return errors.Wrap(err, "")
	}

	return nil
}

func (d *Pool) Retry(op func() error) error {
	if d.MaxWait == 0 {
		d.MaxWait = time.Minute / 2
	}
	bo := backoff.NewExponentialBackOff()
	bo.MaxInterval = time.Second * 5
	bo.MaxElapsedTime = d.MaxWait
	return backoff.Retry(op, bo)
}
