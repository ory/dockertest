package dockertest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

func TestParseImageName(t *testing.T) {
	assert := assert.New(t)

	tests := []struct {
		name string
		repo string
		tag  string
	}{
		{
			name: "postgres",
			repo: "postgres",
			tag:  "",
		},
		{
			name: "postgres:9.4.6",
			repo: "postgres",
			tag:  "9.4.6",
		},
		{
			name: "postgres:1:2",
			repo: "postgres",
			tag:  "1:2",
		},
		{
			name: "",
			repo: "",
			tag:  "",
		},
	}

	for idx, tt := range tests {
		indexStr := fmt.Sprintf("test index: %d", idx)
		repo, tag := parseImageName(tt.name)
		assert.Equal(tt.repo, repo, indexStr)
		assert.Equal(tt.tag, tag, indexStr)
	}
}

func TestDockerImagesContains(t *testing.T) {
	assert := assert.New(t)

	images := dockerImageList{
		dockerImage{repo: "postgres", tag: "latest"},
		dockerImage{repo: "postgres", tag: "9.4.6"},
	}

	tests := []struct {
		repo     string
		tag      string
		contains bool
	}{
		{
			repo:     "postgres",
			tag:      "latest",
			contains: true,
		},
		{
			repo:     "postgres",
			tag:      "",
			contains: true,
		},
		{
			repo:     "postgres",
			tag:      "9.4.6",
			contains: true,
		},
		{
			repo:     "postgres",
			tag:      "9.4",
			contains: false,
		},
		{
			repo:     "postgres1",
			tag:      "",
			contains: false,
		},
		{
			repo:     "",
			tag:      "",
			contains: false,
		},
	}

	for idx, tt := range tests {
		indexStr := fmt.Sprintf("test index: %d", idx)
		assert.Equal(tt.contains, images.contains(tt.repo, tt.tag), indexStr)
	}
}

func TestParseDockerImagesOutput(t *testing.T) {
	assert := assert.New(t)

	normalOutput := []byte(`REPOSITORY          TAG                 IMAGE ID                                                                  CREATED             VIRTUAL SIZE
postgres            latest              sha256:da194fb234df1b69ac5d93032c2f9304ba1d6d85b1a8b5dd94824c4978d0b3d9   2 weeks ago         264.1 MB
postgres            9.4.6               sha256:ad2fc7b9d681789490dfc6b91ef446fc23268572df328158ab542255311a7359   2 weeks ago         263.1 MB
`)

	assert.Equal(
		dockerImageList{
			dockerImage{repo: "postgres", tag: "latest"},
			dockerImage{repo: "postgres", tag: "9.4.6"},
		},
		parseDockerImagesOutput(normalOutput),
	)

	zeroOutput := []byte(`REPOSITORY          TAG                 IMAGE ID            CREATED             VIRTUAL SIZE
`)
	assert.Empty(parseDockerImagesOutput(zeroOutput))

	emptyOutput := []byte{}
	assert.Empty(parseDockerImagesOutput(emptyOutput))
}
