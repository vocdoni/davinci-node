package log

import (
	"errors"
	"io"
	"testing"
	"time"
)

var (
	sampleInt      = 3
	sampleBytes    = []byte("123")
	sampleList     = []int64{10, 0, -10}
	sampleDuration = time.Second
	sampleTime     = time.Unix(12345678, 0)

	errSample = errors.New("some error")
)

func doLogs() {
	// Some sample logs from existing code.
	Infof("added %d keys to census %x", sampleInt, sampleBytes)
	Debugw("importing census", "root", "abc123", "type", "type1")
	Errorf("cannot commit to blockstore: %v", errSample)
	Warnw("various types",
		"list", sampleList,
		"duration", sampleDuration,
		"time", sampleTime,
	)
	Error(errSample)
}

func TestCheckInvalidChars(t *testing.T) {
	t.Cleanup(func() { panicOnInvalidChars = false })

	v := []byte{'h', 'e', 'l', 'l', 'o', 0xff, 'w', 'o', 'r', 'l', 'd'}
	panicOnInvalidChars = false
	Init("debug", "stderr", nil)
	Debugf("%s", v)
	// should not panic since env var is false. if it panics, test will fail

	// now enable panic and try again: should recover() and never reach t.Errorf()
	panicOnInvalidChars = true
	Init("debug", "stderr", nil)
	defer func() { _ = recover() }()
	Debugf("%s", v)
	t.Errorf("Debugf(%s) should have panicked because of invalid char", v)
}

func BenchmarkLogger(b *testing.B) {
	logTestWriter = io.Discard // to not grow a buffer
	Init("debug", logTestWriterName, nil)

	b.ReportAllocs()
	for b.Loop() {
		doLogs()
	}
}
