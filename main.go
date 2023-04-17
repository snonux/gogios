package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"
)

func main() {
	log.Println("Welcome to Gogios!")

	configFile := flag.String("cfg", "/etc/gogios.json", "The config file")

	flag.Parse()

	config, err := newConfig(*configFile)
	if err != nil {
		panic(err)
	}

	for _, check := range config.Checks {
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(config.CheckTimeoutS)*time.Second)
		defer cancel()

		if output, status := check.execute(ctx); status != ok {
			subject := fmt.Sprintf("GOGIOS %s: %s", codeToString(status), check.Name)
			notify(config, subject, output)
		}
	}
}
