// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Hello, World!")
	// Keep container running for tests
	time.Sleep(5 * time.Minute)
}
