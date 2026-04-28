package txmanager

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
)

func TestWaitTxByIDInvokesAllCallbacks(t *testing.T) {
	c := qt.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tm := &TxManager{
		monitorCtx: ctx,
	}

	firstCalled := make(chan error, 1)
	secondCalled := make(chan error, 1)

	err := tm.WaitTxByID([]byte{0x01}, time.Second,
		func(err error) {
			firstCalled <- err
		},
		func(err error) {
			secondCalled <- err
		},
	)

	c.Assert(err, qt.IsNil)

	select {
	case callbackErr := <-firstCalled:
		c.Assert(callbackErr, qt.Not(qt.IsNil))
	case <-time.After(time.Second):
		t.Fatal("first callback was not called")
	}

	select {
	case callbackErr := <-secondCalled:
		c.Assert(callbackErr, qt.Not(qt.IsNil))
	case <-time.After(time.Second):
		t.Fatal("second callback was not called")
	}
}
