package dockertest

import (
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

type ServicePortMap map[int]ServicePort

func (spm ServicePortMap) Wait(timeout time.Duration) error {
	for _, svc := range spm {
		if err := netutil.AwaitReachable(svc.String(), timeout); err != nil {
			return err
		}
	}
	return nil
}
