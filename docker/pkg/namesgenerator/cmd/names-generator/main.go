package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/ory/dockertest/docker/pkg/namesgenerator"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	fmt.Println(namesgenerator.GetRandomName(0))
}
