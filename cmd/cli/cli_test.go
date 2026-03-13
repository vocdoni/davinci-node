package main

import (
	"context"
	"testing"
)

func TestHasAddressAlreadyVoted(t *testing.T) {
	// this keepalive can be removed when HasAddressAlreadyVoted is not deadcode anymore
	f := NewCLIServices(context.TODO()).HasAddressAlreadyVoted
	t.Log("f is a func", f == nil)
}
