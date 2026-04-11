package main

import "nxtermd/internal/server"

var version = "dev"

func main() {
	server.Main(version)
}
