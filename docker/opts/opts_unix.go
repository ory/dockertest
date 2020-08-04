// +build !windows

package opts

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

// DefaultHTTPHost Default HTTP Host used if only port is provided to -H flag e.g. dockerd -H tcp://:8080
const DefaultHTTPHost = "localhost"

// MountParser parses mount path.
func MountParser(mount string) (source, destination string, err error) {
	sd := strings.Split(mount, ":")
	if len(sd) == 2 {
		return sd[0], sd[1], nil
	}
	return "", "", errors.Wrap(fmt.Errorf("invalid mount format: got %s, expected <src>:<dst>", mount), "")
}

// VolumeParser parsed volume path.
func VolumeParser(volume string) (source, target string, err error) {
	st := strings.Split(volume, ":")
	if len(st) == 3 {
		return st[1], st[2], nil
	}
	return "", "", errors.Wrap(fmt.Errorf("invalid volume format: got %s, expected <drive>:<src>:<trgt>", volume), "")
}
