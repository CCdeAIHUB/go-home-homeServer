module gohome/home-server

go 1.25.1

require (
	github.com/gorilla/websocket v1.5.3
	gohome/shared v0.0.0
)

require (
	github.com/tjfoc/gmsm v1.4.1 // indirect
	golang.org/x/crypto v0.0.0-20201012173705-84dcc777aaee // indirect
	golang.org/x/sys v0.36.0 // indirect
)

replace gohome/shared => ../shared/go
