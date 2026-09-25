package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/snonux/gogios/internal"
)

func main() {
	configFile := flag.String("cfg", "/etc/gogios.json", "The config file")
	timeout := flag.Int("timeout", 5, "Timeout of the checks in minutes, counted from when the run lock is held")
	lockWait := flag.Int("lockwait", 5, "Minutes -renotify and -force wait for the run lock")
	renotify := flag.Bool("renotify", false, "Renotify all unhandled")
	force := flag.Bool("force", false, "Force sending out status")
	version := flag.Bool("version", false, "Display version")
	flag.Parse()

	if *version {
		fmt.Print(internal.VersionBanner())
		return
	}

	opts := internal.RunOptions{
		ConfigFile: *configFile,
		Renotify:   *renotify,
		Force:      *force,
		LockWait:   time.Duration(*lockWait) * time.Minute,
		Timeout:    time.Duration(*timeout) * time.Minute,
	}
	if err := internal.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error running gogios: %v\n", err)
		os.Exit(1)
	}
}
