package portmap

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackpal/gateway"
	natpmp "github.com/jackpal/go-nat-pmp"
)

func MaintainNATPMP(ctx context.Context, port uint16) {
	for {
		err := AddNATPMP(port)
		if err != nil {
			log.Printf("NAT-PMP UDP port mapping unavailable: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(refreshMapping):
		}
	}
}

func AddNATPMP(port uint16) error {
	gatewayIP, err := gateway.DiscoverGateway()
	if err != nil {
		return err
	}
	client := natpmp.NewClientWithTimeout(gatewayIP, 3*time.Second)
	result, err := client.AddPortMapping("udp", int(port), int(port), int(mappingLease))
	if err != nil {
		return err
	}
	if result.MappedExternalPort != port {
		_, _ = client.AddPortMapping("udp", int(port), 0, 0)
		return fmt.Errorf("router mapped UDP %d to unusable external port %d", port, result.MappedExternalPort)
	}
	log.Printf("NAT-PMP UDP port mapping active: external=%d internal=%d lifetime=%ds", port, port, result.PortMappingLifetimeInSeconds)
	return nil
}
