//go:build !linux

package main

import "fmt"

func newHomeLink(_, _ string, _ func([]byte) error) (packetLink, error) {
	return nil, fmt.Errorf("home TUN path requires Linux")
}
