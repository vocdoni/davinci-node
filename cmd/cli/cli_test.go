package main

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
)

func TestHasAddressAlreadyVoted(t *testing.T) {
	// this keepalive can be removed when HasAddressAlreadyVoted is not deadcode anymore
	t.Log(NewCLIServices(context.TODO()).HasAddressAlreadyVoted(types.ProcessID{}, common.Address{}))
}
