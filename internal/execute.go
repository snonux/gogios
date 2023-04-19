package internal

import (
	"context"
	"log"
	"sync"
	"time"
)

func execute(globalCtx context.Context, state state, config config) state {
	limiterCh := make(chan struct{}, config.CheckConcurrency)
	inputCh := make(chan namedCheck)
	outputCh := make(chan checkResult)

	go func() {
		for name, check := range config.Checks {
			inputCh <- namedCheck{check, name}
		}
		close(inputCh)
	}()

	var outputWg sync.WaitGroup
	outputWg.Add(1)

	go func() {
		for checkResult := range outputCh {
			state.update(checkResult)
		}
		outputWg.Done()
	}()

	var inputWg sync.WaitGroup
	inputWg.Add(len(config.Checks))

	for check := range inputCh {
		go func(check namedCheck) {
			limiterCh <- struct{}{}
			defer func() {
				<-limiterCh
				inputWg.Done()
			}()

			ctx, cancel := context.WithTimeout(globalCtx,
				time.Duration(config.CheckTimeoutS)*time.Second)
			defer cancel()

			outputCh <- check.execute(ctx)
		}(check)
	}

	inputWg.Wait()
	log.Println("All checks completed!")
	close(outputCh)

	outputWg.Wait()
	log.Println("All outputs collected!")

	return state
}
