package main

import (
	"context"
	"flag"
	"log"
	"sync"
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

	type entry struct {
		name  string
		check check
	}

	limiterCh := make(chan struct{}, config.CheckConcurrency)
	checkCh := make(chan entry)

	go func() {
		for name, check := range config.Checks {
			checkCh <- entry{name, check}
		}
		close(checkCh)
	}()

	var wg sync.WaitGroup
	wg.Add(len(config.Checks))

	for entry := range checkCh {
		go func(name string, check check) {
			limiterCh <- struct{}{}
			defer func() {
				<-limiterCh
				wg.Done()
			}()

			ctx, cancel := context.WithTimeout(context.Background(),
				time.Duration(config.CheckTimeoutS)*time.Second)
			defer cancel()

			output, status := check.execute(ctx)
			// TODO: Send the results through a channel, so we dont have to put a mutex
			// into state.
			state.update(name, output, status)
		}(entry.name, entry.check)
	}

	wg.Wait()
	log.Println("All checks completed!")

	if err := state.persist(); err != nil {
		notifyError(config, err)
	}

	if subject, body, changed := state.report(); changed {
		notify(config, subject, body)
	}
}
