package internal

import (
	"context"
	"log"
	"math/rand"
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
		if age := state.age(check.name); check.RunInterval > int(age.Seconds()) {
			lastCheckState, ok := state.checks[check.name]
			if ok {
				log.Printf("Skipping %s: interval not yet reached (%v (%v) <= %v)", check.name,
					int(age.Seconds()), age, check.RunInterval)
				outputCh <- checkResult{
					name:      check.name,
					output:    lastCheckState.output,
					epoch:     lastCheckState.Epoch,
					status:    lastCheckState.Status,
					federated: lastCheckState.federated,
				}
				inputWg.Done()
				continue
			}
			log.Println("Something went wrong... expected check state for", check,
				"bug got nothing! Proceeding anyway")
		}

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

func runCheck(ctx context.Context, limitCh chan struct{}, deps dependency,
	check namedCheck, conf config, retries int,
) checkResult {
	if err := deps.wait(ctx, check.DependsOn); err != nil {
		deps.notOk(check.name)
		return check.skip(err.Error())
	}

	if check.RandomSpread > 0 {
		d := time.Duration(rand.Intn(check.RandomSpread)) * time.Second
		log.Printf("Sleeping %v before running %s", d, check.name)
		time.Sleep(d)
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