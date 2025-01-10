//go:build !((arm64 || amd64) && !openbsd)

package pebble

import (
	"errors"

	"github.com/paxosglobal/go-ethereum-arbitrum/ethdb"
)

func New(file string, cache int, handles int, namespace string, readonly bool, ephemeral bool, extraOptions *ExtraOptions) (ethdb.Database, error) {
	return nil, errors.New("pebble is not supported on this platform")
}
