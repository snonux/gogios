package internal

import (
	"context"
	"log"
	"sync"
	"time"
)

func runChecks(globalCtx context.Context, state state, config config) state {
	limiterCh := make(chan struct{}, config.CheckConcurrency)
	inputCh := make(chan namedCheck)
	outputCh := make(chan checkResult)
	deps := newDependency(config)

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
			defer inputWg.Done()

			if err := deps.wait(globalCtx, check.DependsOn); err != nil {
				deps.notOk(check.name)
				outputCh <- check.skip(err.Error())
				return
			}

			limiterCh <- struct{}{}
			defer func() { <-limiterCh }()

			ctx, cancel := context.WithTimeout(globalCtx,
				time.Duration(config.CheckTimeoutS)*time.Second)
			defer cancel()

			checkResult := check.run(ctx)

			if checkResult.status == critical {
				deps.notOk(check.name)
			} else {
				deps.ok(check.name)
			}

			outputCh <- checkResult
		}(check)
	}

	inputWg.Wait()
	log.Println("All checks completed!")
	close(outputCh)

	outputWg.Wait()
	log.Println("All outputs collected!")

	return state
}
