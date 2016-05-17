package dockertest

import (
	"bytes"
	"net"
	"text/template"
	"time"

	"camlistore.org/pkg/netutil"
)

// A structure representing
type PublicPort struct {
	Host string `json:"HostIP"`
	Port string `json:"HostPort"`
}

func (sp PublicPort) String() string {
	return net.JoinHostPort(sp.Host, sp.Port)
}

type PortMap map[int]PublicPort

func (spm PortMap) Wait(timeout time.Duration) error {
	for _, svc := range spm {
		if err := netutil.AwaitReachable(svc.String(), timeout); err != nil {
			return err
		}
	}
	return nil
}

type serviceSpec struct {
	port        int
	urlTemplate *template.Template
}

type SimpleServiceMap map[string]serviceSpec

func (p SimpleServiceMap) PublishedPorts() (ports []int) {
	for _, spec := range p {
		ports = append(ports, spec.port)
	}
	return ports
}

func (p SimpleServiceMap) Map(m PortMap) (ServiceURLMap, error) {
	res := ServiceURLMap{}
	for name, spec := range p {
		var url bytes.Buffer
		if err := spec.urlTemplate.Execute(&url, m[spec.port]); err != nil {
			return ServiceURLMap{}, err
		}
		res[name] = url.String()
	}
	return res, nil
}

// Defines a service by name and a urlTemplate, which is a text/Template called
// with Port for context
func SimpleService(port int, urlTemplate string) serviceSpec {
	return serviceSpec{
		port:        port,
		urlTemplate: template.Must(template.New("").Parse(urlTemplate)),
	}
}
