package internal

import (
	"context"
	"log"
	"sync"
	"time"
)

func runChecks(ctx context.Context, state state, config config) state {
	limitCh := make(chan struct{}, config.CheckConcurrency)
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
			outputCh <- runCheck(ctx, limitCh, deps, check, config, check.Retries)
			inputWg.Done()
		}(check)
	}

	inputWg.Wait()
	log.Println("All checks completed!")
	close(outputCh)

	outputWg.Wait()
	log.Println("All outputs collected!")

	return state
}

func runCheck(ctx context.Context, limitCh chan struct{},
	deps dependency, check namedCheck, config config, retries int) checkResult {

	if err := deps.wait(ctx, check.DependsOn); err != nil {
		deps.notOk(check.name)
		return check.skip(err.Error())
	}

	limitCh <- struct{}{}

	checkCtx, cancel := context.WithTimeout(ctx,
		time.Duration(config.CheckTimeoutS)*time.Second)
	defer cancel()

	checkResult := check.run(checkCtx)

	if checkResult.status != ok && retries > 0 {
		<-limitCh
		retryDuration := time.Duration(check.RetryInterval) * time.Second
		time.Sleep(retryDuration)
		log.Printf("Retrying %s after %v", check.name, retryDuration)
		return runCheck(ctx, limitCh, deps, check, config, retries-1)
	}

	if checkResult.status == critical {
		deps.notOk(check.name)
	} else {
		deps.ok(check.name)
	}

	<-limitCh
	return checkResult
}
