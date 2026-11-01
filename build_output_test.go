// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"strings"
	"testing"
)

func TestReadBuildResultWritesOutput(t *testing.T) {
	stream := `{"stream":"Step 1/2 : FROM alpine\n"}` + "\n" +
		`{"stream":"syntax error: unexpected name c\n"}` + "\n" +
		`{"errorDetail":{"message":"returned a non-zero code: 1"},"error":"returned a non-zero code: 1"}` + "\n"
	var out strings.Builder
	if _, err := readBuildResult(strings.NewReader(stream), &out); err == nil {
		t.Fatal("readBuildResult() error = nil, want the build error")
	}
	if want := "Step 1/2 : FROM alpine\nsyntax error: unexpected name c\n"; out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}
