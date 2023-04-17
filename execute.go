package main

import (
	"context"
	"log"
	"sync"
	"time"
)

type executionUnit struct {
	name  string
	check check
}

func execute(config config, state state) state {
	limiterCh := make(chan struct{}, config.CheckConcurrency)
	executionCh := make(chan executionUnit)
	resultCh := make(chan checkResult)

	go func() {
		for name, check := range config.Checks {
			executionCh <- executionUnit{name, check}
		}
		close(executionCh)
	}()

	var resultWg sync.WaitGroup
	resultWg.Add(1)

	go func() {
		for checkResult := range resultCh {
			state.update(checkResult)
		}
		resultWg.Done()
	}()

	var executionWg sync.WaitGroup
	executionWg.Add(len(config.Checks))

	for executionUnit := range executionCh {
		go func(name string, check check) {
			limiterCh <- struct{}{}
			defer func() {
				<-limiterCh
				executionWg.Done()
			}()

			ctx, cancel := context.WithTimeout(context.Background(),
				time.Duration(config.CheckTimeoutS)*time.Second)
			defer cancel()

			resultCh <- check.execute(ctx, name)
		}(executionUnit.name, executionUnit.check)
	}

	executionWg.Wait()
	log.Println("All checks completed!")
	close(resultCh)

	resultWg.Wait()
	log.Println("All results collected!")

	return state
}
