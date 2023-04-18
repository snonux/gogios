package main

import (
	"flag"

	"codeberg.org/snonux/gogios/internal"
)

func main() {
	configFile := flag.String("cfg", "/etc/gogios.json", "The config file")
	flag.Parse()
	internal.Run(*configFile)
}
