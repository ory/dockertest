package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientInterface(t *testing.T) {
	// Verify MobyClient implements Client interface
	var _ Client = (*MobyClient)(nil)
	// Verify MockClient implements Client interface
	var _ Client = (*MockClient)(nil)
}

func TestMobyClientCreation(t *testing.T) {
	client, err := NewMobyClient()
	assert.NoError(t, err)
	assert.NotNil(t, client)

	err = client.Close()
	assert.NoError(t, err)
}

func TestMobyClientPing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cli, err := NewMobyClient()
	assert.NoError(t, err)
	defer cli.Close()

	ctx := context.Background()
	ping, err := cli.Ping(ctx)
	assert.NoError(t, err)
	assert.NotEmpty(t, ping.APIVersion)
}
