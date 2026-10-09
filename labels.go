// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"maps"
	"strconv"
)

// Ownership labels are reserved under the io.ory.dockertest namespace. They
// are added to every container, network, and built image so that resources can
// be attributed to a process run and recovered after that run has died.
const (
	labelTrue    = "true"
	labelManaged = "io.ory.dockertest.managed" // "true" for every resource dockertest created
	labelRun     = "io.ory.dockertest.run"     // unique ID of the creating process run; absent on retained images
	labelHost    = "io.ory.dockertest.host"    // local host identity of the creating process
	labelScope   = "io.ory.dockertest.scope"   // project scope passed to Main; absent without Main
	labelRetain  = "io.ory.dockertest.retain"  // "true" on build images that are kept as a cache, "false" otherwise
)

// withOwnershipLabels returns a copy of user with the ownership labels of this
// process added. The caller's map is never modified. Retained images carry
// stable cache labels without a run ID so they survive across runs and are
// excluded from recovery. The retain label is always set explicitly: an image
// inherits the labels of its base image, so a build FROM a retained image
// would otherwise be retained, too.
func (o *processOwner) withOwnershipLabels(user map[string]string, retain bool) map[string]string {
	labels := make(map[string]string, len(user)+5)
	maps.Copy(labels, user)
	maps.Copy(labels, runLabels(o.scopeName(), o.hostID, o.runID))
	labels[labelRetain] = strconv.FormatBool(retain)
	if retain {
		delete(labels, labelRun)
	}
	return labels
}

// runLabels is the ownership tuple that identifies the resources of one run.
// The scope label is only present under Main.
func runLabels(scope, host, runID string) map[string]string {
	labels := map[string]string{labelManaged: labelTrue, labelHost: host, labelRun: runID}
	if scope != "" {
		labels[labelScope] = scope
	}
	return labels
}

// ownershipMatches reports whether every label in want is present with the same
// value in have. It is the only test used before deleting a resource that
// dockertest did not create in this process.
func ownershipMatches(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}
