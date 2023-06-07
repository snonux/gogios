package internal

import (
	"context"
	"log"
	"sync"
	"time"
)

func runChecks(ctx context.Context, state state, conf config) state {
	var (
		limitCh  = make(chan struct{}, conf.CheckConcurrency)
		inputCh  = make(chan namedCheck)
		outputCh = make(chan checkResult)
		deps     = newDependency(conf)
	)

	go func() {
		for name, check := range conf.Checks {
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
	inputWg.Add(len(conf.Checks))

	for check := range inputCh {
		go func(check namedCheck) {
			outputCh <- runCheck(ctx, limitCh, deps, check, conf, check.Retries)
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
	deps dependency, check namedCheck, conf config, retries int) checkResult {

	if err := deps.wait(ctx, check.DependsOn); err != nil {
		deps.notOk(check.name)
		return check.skip(err.Error())
	}

	limitCh <- struct{}{}

	checkCtx, cancel := context.WithTimeout(ctx,
		time.Duration(conf.CheckTimeoutS)*time.Second)
	defer cancel()

	checkResult := check.run(checkCtx)

	if checkResult.status != nagiosOk && retries > 0 {
		<-limitCh
		retryDuration := time.Duration(check.RetryInterval) * time.Second
		time.Sleep(retryDuration)
		log.Printf("Retrying %s after %v", check.name, retryDuration)
		return runCheck(ctx, limitCh, deps, check, conf, retries-1)
	}

	if checkResult.status == nagiosCritical {
		deps.notOk(check.name)
	} else {
		deps.ok(check.name)
	}

	<-limitCh
	return checkResult
}
