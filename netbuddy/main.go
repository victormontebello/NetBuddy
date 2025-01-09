package main

import (
	"module/settings"
	peeringdb "module/usecases/peering"
	"module/usecases/ping"
	scan "module/usecases/scanner"
	//"module/usecases/speedtest"
)

func main() {
	cfg, err := settings.ReadDefaultConfig()
	if err != nil {
		return
	}

	peeringdb.Search("6327")

	scan.ScanPorts(cfg, "www.google.com")

	pg, err := ping.NewPing("www.google.com", cfg)

	if err != nil {
		return
	}

	pg.Run()

}
