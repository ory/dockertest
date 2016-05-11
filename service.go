package dockertest

import (
	"fmt"
	"net"
	"time"

	"camlistore.org/pkg/netutil"
)

// A structure representing
type ServicePort struct {
	Host string `json:"HostIP"`
	Port string `json:"HostPort"`
}

func (sp ServicePort) String() string {
	return net.JoinHostPort(sp.Host, sp.Port)
}

type ServicePorts []ServicePort

func (svcs ServicePorts) First() ServicePort {
	if len(svcs) == 0 {
		panic("ServicePorts.First(): empty list")
	}
	return svcs[0]
}

func (svcs ServicePorts) Wait(timeout time.Duration) error {
	for _, svc := range svcs {
		if err := netutil.AwaitReachable(svc.String(), timeout); err != nil {
			return err
		}
	}
	return nil
}

type ServicePortMap map[int]ServicePort

func (spm ServicePortMap) Ordered(order []int) (ServicePorts, error) {
	res := make(ServicePorts, len(order))
	for i, p := range order {
		var ok bool
		res[i], ok = spm[p]
		if !ok {
			return ServicePorts{}, fmt.Errorf("Port %d not found", p)
		}
	}
	return res, nil
}
