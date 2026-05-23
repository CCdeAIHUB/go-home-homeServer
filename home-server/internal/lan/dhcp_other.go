//go:build !linux

package lan

import (
	"context"
	"fmt"
)

// Lease 表示 DHCP 代理获取的 IP 租约。
// 非 Linux 平台的桩实现，不支持 DHCP 代理功能。
type Lease struct {
	IP        string
	Netmask   string
	Gateway   string
	DNS       []string
	ExpiresAt interface{}
}

// Release 释放 DHCP 租约（非 Linux 平台为桩实现）。
func (l *Lease) Release() {
	// 非 Linux 平台不支持 DHCP 代理
}

// RequestLease 请求 DHCP 租约（非 Linux 平台返回错误）。
func RequestLease(_ context.Context, _, _ string) (*Lease, error) {
	return nil, fmt.Errorf("DHCP proxy lease requires Linux")
}
