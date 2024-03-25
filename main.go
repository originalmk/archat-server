package main

import (
	"log"
	"os"

	"krzyzanowski.dev/p2pchat/client"
	"krzyzanowski.dev/p2pchat/server"
)

func main() {
	args := os.Args[1:]

	if len(args) != 1 {
		log.Fatalln("You must provide only one argument which is type of " +
			"application: 'server' or 'client'")
	}

	runType := args[0]

	if runType == "client" {
		client.RunClient()
	} else if runType == "server" {
		server.RunServer()
	} else {
		log.Fatalf("Unknown run type %s\n", runType)
	}
}
