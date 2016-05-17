package dockertest

import (
	"io"
	"sync"
)

// A single-writer, multi-reader, history-preserving buffer implementation.
// Data written through Write() will immediately pop-out on waiting Reader:s
// Late-created readers will start again from the beginning
type logBuffer struct {
	m      sync.RWMutex
	c      *sync.Cond
	buf    []byte
	closed bool
}

func newLog() *logBuffer {
	l := &logBuffer{}
	l.c = sync.NewCond(l.m.RLocker())
	return l
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.m.Lock()
	defer l.m.Unlock()
	l.buf = append(l.buf, p...)
	l.c.Broadcast()

	return len(p), nil
}

func (l *logBuffer) Close() error {
	l.m.Lock()
	defer l.m.Unlock()
	l.closed = true
	l.c.Broadcast()

	return nil
}

func (l *logBuffer) Reader() *logReader {
	return &logReader{l, 0}
}

type logReader struct {
	log *logBuffer
	pos int
}

func (l *logReader) HasMore() bool {
	return l.pos < len(l.log.buf)
}

func (l *logReader) Read(tgt []byte) (int, error) {
	l.log.m.RLock()
	defer l.log.m.RUnlock()
	for !l.HasMore() {
		if l.log.closed {
			return 0, io.EOF
		}
		l.log.c.Wait()
	}
	copied := copy(tgt, l.log.buf[l.pos:])
	l.pos += copied
	return copied, nil
}
