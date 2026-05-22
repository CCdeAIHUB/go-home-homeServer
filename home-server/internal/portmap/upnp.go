package portmap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

const (
	mappingLease   = uint32(60 * 60)
	refreshMapping = 45 * time.Minute
)

type upnpMapper interface {
	AddPortMappingCtx(context.Context, string, uint16, string, uint16, string, bool, string, uint32) error
}

func MaintainUPnP(ctx context.Context, port uint16, iface string) {
	for {
		mapCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		err := AddUPnP(mapCtx, port, iface)
		cancel()
		if err != nil {
			log.Printf("UPnP UDP port mapping unavailable: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(refreshMapping):
		}
	}
}

func AddUPnP(ctx context.Context, port uint16, iface string) error {
	internalClient, err := localIPv4(iface)
	if err != nil {
		return err
	}
	mappers, discoveryErrs := discoverUPnPMappers(ctx)
	if len(mappers) == 0 {
		if joined := errors.Join(discoveryErrs...); joined != nil {
			return fmt.Errorf("discover UPnP WAN service: %w", joined)
		}
		return errors.New("router exposes no UPnP WAN port mapping service")
	}

	var mappingErrs []error
	for _, mapper := range mappers {
		if err := mapper.AddPortMappingCtx(ctx, "", port, "UDP", port, internalClient, true, "Go Home UDP", mappingLease); err == nil {
			log.Printf("UPnP UDP port mapping active: external=%d internal=%s:%d", port, internalClient, port)
			return nil
		} else {
			mappingErrs = append(mappingErrs, err)
		}
	}
	return fmt.Errorf("map UDP port %d with UPnP: %w", port, errors.Join(mappingErrs...))
}

func discoverUPnPMappers(ctx context.Context) ([]upnpMapper, []error) {
	var mappers []upnpMapper
	var errs []error

	ip2, ip2Errs, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx)
	errs = appendDiscovery(errs, err, ip2Errs)
	for _, mapper := range ip2 {
		mappers = append(mappers, mapper)
	}

	ip21, ip21Errs, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx)
	errs = appendDiscovery(errs, err, ip21Errs)
	for _, mapper := range ip21 {
		mappers = append(mappers, mapper)
	}

	ppp2, ppp2Errs, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx)
	errs = appendDiscovery(errs, err, ppp2Errs)
	for _, mapper := range ppp2 {
		mappers = append(mappers, mapper)
	}

	ip1, ip1Errs, err := internetgateway1.NewWANIPConnection1ClientsCtx(ctx)
	errs = appendDiscovery(errs, err, ip1Errs)
	for _, mapper := range ip1 {
		mappers = append(mappers, mapper)
	}

	ppp1, ppp1Errs, err := internetgateway1.NewWANPPPConnection1ClientsCtx(ctx)
	errs = appendDiscovery(errs, err, ppp1Errs)
	for _, mapper := range ppp1 {
		mappers = append(mappers, mapper)
	}

	return mappers, errs
}

func appendDiscovery(errs []error, err error, nested []error) []error {
	if err != nil {
		errs = append(errs, err)
	}
	return append(errs, nested...)
}

func localIPv4(interfaceName string) (string, error) {
	if interfaceName != "" {
		iface, err := net.InterfaceByName(interfaceName)
		if err != nil {
			return "", err
		}
		if ip := interfaceIPv4(iface); ip != "" {
			return ip, nil
		}
		return "", fmt.Errorf("interface %s has no private IPv4 address", interfaceName)
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if ip := interfaceIPv4(&iface); ip != "" {
			return ip, nil
		}
	}
	return "", errors.New("no private IPv4 interface is available for UPnP")
}

func interfaceIPv4(iface *net.Interface) string {
	if iface == nil || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip != nil && ip.IsPrivate() {
			return ip.String()
		}
	}
	return ""
}
