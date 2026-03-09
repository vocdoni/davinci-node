package sequencer

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
)

func TestFinalizerCloseWaitsForInFlightFinalization(t *testing.T) {
	c := qt.New(t)

	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	f := &finalizer{
		OndemandCh: make(chan types.ProcessID, 1),
		ctx:        ctx,
		cancel:     cancel,
	}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		close(started)
		<-release
	}()
	<-started

	done := make(chan struct{})
	go func() {
		f.Close()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Close returned before in-flight finalization completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after finalization completed")
	}

	c.Assert(f.cancel, qt.IsNil)
}

func TestSequencerStopWaitsForFinalizerClose(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	f := &finalizer{
		OndemandCh: make(chan types.ProcessID, 1),
		ctx:        ctx,
		cancel:     cancel,
	}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		close(started)
		<-release
	}()

	s := &Sequencer{
		finalizer: f,
		cancel:    cancel,
	}
	<-started

	done := make(chan struct{})
	go func() {
		_ = s.Stop()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Stop returned before finalizer finished")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after finalizer finished")
	}
}
