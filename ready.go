package dockertest

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"
)

type regexWaiter struct {
	regexes []*regexp.Regexp
}

// Waiter that scans the log of the container from a given list of regular
// expressions, one at a time. Service is considered ready when all regexes
// have been found
func RegexWaiter(regexes ...string) regexWaiter {
	if len(regexes) == 0 {
		panic("RegexWaiter requires at least 1 regex to run")
	}
	res := regexWaiter{}
	for _, s := range regexes {
		res.regexes = append(res.regexes, regexp.MustCompile(s))
	}
	return res
}

func (r regexWaiter) WaitForReady(c Container) error {
	var err error
	var line string
	l := bufio.NewReader(c.Log())
	remaining := r.regexes
	for line, err = l.ReadString('\n'); err == nil; line, err = l.ReadString('\n') {
		if remaining[0].MatchString(line) {
			remaining = remaining[1:]
			if len(remaining) == 0 {
				return nil
			}
		}
	}
	if err == io.EOF {
		completeLog, err := ioutil.ReadAll(c.Log())
		if err != nil {
			return fmt.Errorf("Unexpected error while slurping log: %s", err)
		}
		return fmt.Errorf("Expected line was not found in log: %s\n\n %s", remaining[0].String(), string(completeLog))
	} else {
		return fmt.Errorf("Unexpected error while reading stdout: %s", err)
	}
}
