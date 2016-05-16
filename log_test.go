package dockertest

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLog(t *testing.T) {
	assert := assert.New(t)

	test1 := "something"
	test2 := " completely different"
	buf := make([]byte, 64)

	log := NewLog()
	log.Write([]byte(test1))

	reader := log.Reader()
	assert.True(reader.HasMore())
	n, err := reader.Read(buf)
	assert.Nil(err)
	assert.Equal(n, len(test1))
	assert.Equal(string(buf[:n]), test1)
	assert.False(reader.HasMore())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		n, err = reader.Read(buf)
		wg.Done()
	}()
	go func() {
		time.Sleep(time.Millisecond * 100)
		log.Write([]byte(test2))
		log.Close()
		wg.Done()
	}()
	wg.Wait()

	assert.Nil(err)
	assert.Equal(n, len(test2))
	assert.Equal(string(buf[:n]), test2)
	assert.False(reader.HasMore())

	reader = log.Reader()
	assert.True(reader.HasMore())
	n, err = reader.Read(buf)
	assert.Nil(err)
	assert.Equal(n, len(test1)+len(test2))
	assert.Equal(string(buf[:n]), test1+test2)
	assert.False(reader.HasMore())

	n, err = reader.Read(buf)
	assert.Equal(err, io.EOF)
}
