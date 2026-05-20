package txmanager

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	qt "github.com/frankban/quicktest"
	"github.com/holiman/uint256"
	"github.com/vocdoni/davinci-node/types"
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

func TestTrackTxStoresBlobSidecarImmediately(t *testing.T) {
	c := qt.New(t)

	blobData := bytes.Repeat([]byte{0x01}, types.BlobLength)
	blob := types.MustBlobFromBytes(blobData)
	sidecar, err := types.ComputeBlobTxSidecar(types.BlobTxSidecarVersion0, []*types.Blob{blob})
	c.Assert(err, qt.IsNil)

	to := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx := gethtypes.NewTx(&gethtypes.BlobTx{
		ChainID:    uint256.NewInt(1),
		Nonce:      7,
		GasTipCap:  uint256.NewInt(1),
		GasFeeCap:  uint256.NewInt(2),
		Gas:        21000,
		To:         to,
		Value:      uint256.NewInt(0),
		Data:       []byte("blob"),
		BlobFeeCap: uint256.NewInt(3),
		BlobHashes: sidecar.BlobHashes(),
		Sidecar:    sidecar.AsGethSidecar(),
	})

	tm := &TxManager{
		pendingTxs: make(map[uint64]*PendingTransaction),
	}
	tm.trackTx([]byte{0x01}, tx)

	ptx := tm.pendingTxs[7]
	c.Assert(ptx, qt.Not(qt.IsNil))
	c.Assert(ptx.IsBlob, qt.IsTrue)
	c.Assert(ptx.BlobSidecar, qt.Not(qt.IsNil))
	c.Assert(ptx.BlobSidecar.Version, qt.Equals, sidecar.Version)
	c.Assert(ptx.BlobSidecar.BlobHashes(), qt.DeepEquals, sidecar.BlobHashes())
}
