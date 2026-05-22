//go:build !linux

package lan

import (
	"context"
	"fmt"
	"time"
)

type Lease struct {
	IP        string
	Netmask   string
	Gateway   string
	DNS       []string
	ExpiresAt time.Time
}

func RequestLease(_ context.Context, _, _ string) (Lease, error) {
	return Lease{}, fmt.Errorf("DHCP proxy lease requires Linux")
}
