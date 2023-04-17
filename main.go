package main

import (
	"context"
	"flag"
	"fmt"
	"time"
)

func main() {
	configFile := flag.String("cfg", "/etc/gogios.json", "The config file")
	flag.Parse()

	config, err := newConfig(*configFile)
	if err != nil {
		panic(err)
	}

	state, err := newState(config)
	if err != nil {
		notifyError(config, err)
	}

	for name, check := range config.Checks {
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(config.CheckTimeoutS)*time.Second)
		defer cancel()

		output, status := check.execute(ctx)
		stateChanged := state.update(name, status)

		if status != ok || stateChanged {
			subject := fmt.Sprintf("GOGIOS %s: %s", codeToString(status), name)
			notify(config, subject, output)
		}
	}

	if err := state.persist(); err != nil {
		notifyError(config, err)
	}
}
