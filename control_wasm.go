// +build wasm

package p2p

import (
	"syscall"
)

func Control(network, address string, c syscall.RawConn) error {
	return nil
}
