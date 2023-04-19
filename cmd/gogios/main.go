package main

import (
	"context"
	"flag"
	"time"

	"codeberg.org/snonux/gogios/internal"
)

func main() {
	configFile := flag.String("cfg", "/etc/gogios.json", "The config file")
	timeout := flag.Int("timeout", 5, "Global timeout in minutes")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(*timeout)*time.Minute)
	defer cancel()

	internal.Run(ctx, *configFile)
}
