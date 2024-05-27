package main

import (
	"flag"
	"log"

	"krzyzanowski.dev/archat/client"
	"krzyzanowski.dev/archat/common"
	"krzyzanowski.dev/archat/server"
)

func main() {
	wsapiAddr := flag.String("waddr", ":8080", "An IP address of the websocket API endpoint")
	udpAddr := flag.String("uaddr", ":8081", "An IP address of the UDP hole-punching listener")
	runType := flag.String("run", "client", "Either one of 'client' or 'server'")

	flag.Parse()

	commonSettings := common.Settings{
		WsapiAddr: *wsapiAddr,
		UdpAddr:   *udpAddr,
	}

	if *runType == "client" {
		settings := common.ClientSettings{
			Settings: commonSettings,
		}
		client.RunClient(settings)
	} else if *runType == "server" {
		settings := common.ServerSettings{
			Settings: commonSettings,
		}
		server.RunServer(settings)
	} else {
		log.Fatalf("Unknown run type %s\n", *runType)
	}
}
