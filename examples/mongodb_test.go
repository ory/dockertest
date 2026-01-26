// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"github.com/ory/dockertest/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"testing"
	"time"
)

func TestMongoDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	mongodb := pool.RunT(t, "mongo",
		dockertest.WithTag("7"),
	)
	mongodb.Cleanup(t)

	// Create MongoDB client
	ctx := t.Context()
	uri := "mongodb://" + mongodb.GetHostPort("27017/tcp")

	var client *mongo.Client
	err := pool.Retry(ctx, 30*time.Second, func() error {
		var err error
		client, err = mongo.Connect(ctx, options.Client().ApplyURI(uri))
		if err != nil {
			return err
		}
		return client.Ping(ctx, nil)
	})
	if err != nil {
		t.Fatalf("Could not connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	// Insert a document
	collection := client.Database("testdb").Collection("users")
	doc := bson.D{
		{Key: "id", Value: 1},
		{Key: "name", Value: "Alice"},
	}

	_, err = collection.InsertOne(ctx, doc)
	if err != nil {
		t.Fatalf("Failed to insert document: %v", err)
	}

	// Find the document
	var result bson.M
	err = collection.FindOne(ctx, bson.D{{Key: "id", Value: 1}}).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to find document: %v", err)
	}

	name, ok := result["name"].(string)
	if !ok {
		t.Fatalf("Name field is not a string")
	}

	if name != "Alice" {
		t.Errorf("Expected name 'Alice', got '%s'", name)
	}

	t.Logf("MongoDB test successful: retrieved name '%s'", name)
}
