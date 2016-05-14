package dockertest

import (
	"sync"
)

type Log struct {
	m   sync.RWMutex
	c   *sync.Cond
	buf []byte
}

func NewLog() *Log {
	l := &Log{}
	l.c = sync.NewCond(l.m.RLocker())
	return l
}

func (l *Log) Write(p []byte) (int, error) {
	l.m.Lock()
	defer l.m.Unlock()
	l.buf = append(l.buf, p...)
	l.c.Broadcast()

	return len(p), nil
}

func (l *Log) Reader() *LogReader {
	return &LogReader{l, 0}
}

type LogReader struct {
	log *Log
	pos int
}

func (l *LogReader) HasMore() bool {
	return l.pos < len(l.log.buf)
}

func (l *LogReader) Read(tgt []byte) (int, error) {
	l.log.m.RLock()
	defer l.log.m.RUnlock()
	for !l.HasMore() {
		l.log.c.Wait()
	}
	copied := copy(tgt, l.log.buf[l.pos:])
	l.pos += copied
	return copied, nil
}
