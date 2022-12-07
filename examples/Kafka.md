```go
package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/go-zookeeper/zk"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

func main() {
	dockerPool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	if err = dockerPool.Client.Ping(); err != nil {
		log.Fatalf(`could not connect to docker: %s`, err)
	}

	network, err := dockerPool.Client.CreateNetwork(docker.CreateNetworkOptions{Name: "zookeeper_kafka_network"})
	if err != nil {
		log.Fatalf("could not create a network to zookeeper and kafka: %s", err)
	}

	zookeeperResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:         "zookeeper-example",
		Repository:   "wurstmeister/zookeeper",
		Tag:          "3.4.6",
		NetworkID:    network.ID,
		Hostname:     "zookeeper",
		ExposedPorts: []string{"2181"},
	})
	if err != nil {
		log.Fatalf("could not start zookeeper: %s", err)
	}

	conn, _, err := zk.Connect([]string{fmt.Sprintf("127.0.0.1:%s", zookeeperResource.GetPort("2181/tcp"))}, 10*time.Second)
	if err != nil {
		log.Fatalf("could not connect zookeeper: %s", err)
	}
	defer conn.Close()

	retryFn := func() error {
		switch conn.State() {
		case zk.StateHasSession, zk.StateConnected:
			return nil
		default:
			return errors.New("not yet connected")
		}
	}

	if err = dockerPool.Retry(retryFn); err != nil {
		log.Fatalf("could not connect to zookeeper: %s", err)
	}

	kafkaResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "kafka-example",
		Repository: "wurstmeister/kafka",
		Tag:        "2.12-2.3.0",
		NetworkID:  network.ID,
		Hostname:   "kafka",
		Env: []string{
			"KAFKA_CREATE_TOPICS=domain.test:1:1:compact",
			"KAFKA_ADVERTISED_LISTENERS=INSIDE://kafka:9092,OUTSIDE://localhost:9093",
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=INSIDE:PLAINTEXT,OUTSIDE:PLAINTEXT",
			"KAFKA_LISTENERS=INSIDE://0.0.0.0:9092,OUTSIDE://0.0.0.0:9093",
			"KAFKA_ZOOKEEPER_CONNECT=zookeeper:2181",
			"KAFKA_INTER_BROKER_LISTENER_NAME=INSIDE",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1",
		},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"9093/tcp": {{HostIP: "localhost", HostPort: "9093/tcp"}},
		},
		ExposedPorts: []string{"9093/tcp"},
	})
	if err != nil {
		log.Fatalf("could not start kafka: %s", err)
	}

	retryFn = func() error {
		deliveryChan := make(chan kafka.Event)
		producer, err := kafka.NewProducer(&kafka.ConfigMap{
			"bootstrap.servers": fmt.Sprintf("localhost:%s", kafkaResource.GetPort("9093/tcp")),
			"acks":              "all",
		})
		if err != nil {
			return err
		}
		defer producer.Close()

		topic := "domain.test"
		message := &kafka.Message{
			Key: []byte("any-key"),
			TopicPartition: kafka.TopicPartition{
				Topic:     &topic,
				Partition: kafka.PartitionAny,
			},
			Value: []byte("Hello World"),
		}
		if err = producer.Produce(message, deliveryChan); err != nil {
			return err
		}

		e := <-deliveryChan
		if e.(*kafka.Message).TopicPartition.Error != nil {
			return e.(*kafka.Message).TopicPartition.Error
		}

		return nil
	}

	if err = dockerPool.Retry(retryFn); err != nil {
		log.Fatalf("could not connect to kafka: %s", err)
	}

	if err = dockerPool.Purge(zookeeperResource); err != nil {
		log.Fatalf("could not purge zookeeperResource: %s", err)
	}

	if err = dockerPool.Purge(kafkaResource); err != nil {
		log.Fatalf("could not purge kafkaResource: %s", err)
	}

	if err = dockerPool.Client.RemoveNetwork(network.ID); err != nil {
		log.Fatalf("could not remove %s network: %s", network.Name, err)
	}
}
```
