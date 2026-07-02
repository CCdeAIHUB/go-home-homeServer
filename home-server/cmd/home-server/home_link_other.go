//go:build !linux && !windows

package main

import "fmt"

func newHomeLink(_, _, _ string, _ func([]byte) error) (packetLink, error) {
	return nil, fmt.Errorf("home TUN path requires Linux")
}
