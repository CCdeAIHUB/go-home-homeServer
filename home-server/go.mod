module gohome/home-server

go 1.25.1

require (
	github.com/gorilla/websocket v1.5.3
	gohome/shared v0.0.0
)

require github.com/tjfoc/gmsm v1.4.1 // indirect

replace gohome/shared => ../shared/go
