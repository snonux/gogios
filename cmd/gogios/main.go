package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"codeberg.org/snonux/gogios/internal"
)

const versionStr = "v1.1.0"

func main() {
	configFile := flag.String("cfg", "/etc/gogios.json", "The config file")
	timeout := flag.Int("timeout", 5, "Global timeout in minutes")
	renotify := flag.Bool("renotify", false, "Renotify all unhandled")
	force := flag.Bool("force", false, "Force sending out status")
	version := flag.Bool("version", false, "Display version")
	flag.Parse()

	if *version {
		fmt.Printf("This is Gogios version %s; (C) by Paul Buetow\n", versionStr)
		fmt.Println("https://codeberg.org/snonux/gogios")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(*timeout)*time.Minute)
	defer cancel()

	internal.Run(ctx, *configFile, *renotify, *force)
}
