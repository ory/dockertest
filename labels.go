// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"maps"
	"strconv"
	"time"
)

// Ownership labels are reserved under the io.ory.dockertest namespace. They
// are added to every container and network so that resources can
// be attributed to a process run and swept once that run is provably gone.
const (
	labelTrue    = "true"
	labelManaged = "io.ory.dockertest.managed" // "true" for every resource dockertest created
	labelRun     = "io.ory.dockertest.run"     // unique ID of the creating process run
	labelExpires = "io.ory.dockertest.expires" // unix seconds after which the run is dead; only under Main
)

// withOwnershipLabels copies user labels and adds this process's ownership labels.
func (o *processOwner) withOwnershipLabels(user map[string]string) map[string]string {
	labels := make(map[string]string, len(user)+3)
	maps.Copy(labels, user)
	labels[labelManaged] = labelTrue
	labels[labelRun] = o.runID
	o.mu.Lock()
	expires := o.expires
	o.mu.Unlock()
	// A caller-supplied expiry must not make this run's resources sweepable.
	delete(labels, labelExpires)
	if expires != "" {
		labels[labelExpires] = expires
	}
	return labels
}

// orphaned reports whether a resource with the given labels belongs to
// dockertest and was created either by this run or by a run whose expiry has
// passed at now. A run past its expiry has been killed by the test timeout.
func orphaned(labels map[string]string, runID string, now time.Time) bool {
	if labels[labelManaged] != labelTrue {
		return false
	}
	if labels[labelRun] == runID {
		return true
	}
	expires, err := strconv.ParseInt(labels[labelExpires], 10, 64)
	return err == nil && expires < now.Unix()
}
