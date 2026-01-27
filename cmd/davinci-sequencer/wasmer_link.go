//go:build cgo

package main

import (
	// Ensure libwasmer is linked for rapidsnark witness support.
	_ "github.com/iden3/go-rapidsnark/witness"
)
