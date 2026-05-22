module gohome/home-server

go 1.25.1

require (
	github.com/gorilla/websocket v1.5.3
	gohome/shared v0.0.0
)

require (
	github.com/insomniacslk/dhcp v0.0.0-20260407060928-11b94ed970f2 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20250521234502-f333402bd9cb // indirect
)

replace gohome/shared => ../shared/go
