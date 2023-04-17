package main

import (
	"context"
	"flag"
	"fmt"
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
			stateChanged := state.update(name, status)

			if status != ok || stateChanged {
				subject := fmt.Sprintf("GOGIOS %s: %s", codeToString(status), name)
				notify(config, subject, output)
			}

		}(entry.name, entry.check)
	}

	wg.Wait()
	log.Println("All checks completed!")

	if err := state.persist(); err != nil {
		notifyError(config, err)
	}
}
