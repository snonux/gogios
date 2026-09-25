package internal

import (
	"context"
	"log"
	"maps"
	"math/rand"
	"sync"
	"time"
)

// runChecks runs every configured check and folds the results into state.
// Results are written by one collector goroutine only; the loop that decides
// which checks to skip reads a copy of the previous state taken before the
// collector starts, since reading state.checks while the collector writes it
// is a data race (and can abort the run with a fatal concurrent map access).
func runChecks(ctx context.Context, state state, conf config) state {
	var (
		limitCh  = make(chan struct{}, conf.CheckConcurrency)
		inputCh  = make(chan namedCheck)
		outputCh = make(chan checkResult)
		deps     = newDependency(conf)
		previous = maps.Clone(state.checks)
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
		if result, ok := reuseCheckResult(check, previous, deps); ok {
			outputCh <- result
			inputWg.Done()
			continue
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

// reuseCheckResult returns the previous result of check when its RunInterval
// has not elapsed yet. It settles the check's dependency as runCheck would
// (CRITICAL is not OK, anything else OK), so checks depending on a skipped
// one do not wait for it until the run times out.
func reuseCheckResult(check namedCheck, previous map[string]checkState, deps dependency) (checkResult, bool) {
	last, ok := previous[check.name]
	if !ok {
		return checkResult{}, false
	}
	age := time.Since(time.Unix(last.Epoch, 0))
	if check.RunInterval <= int(age.Seconds()) {
		return checkResult{}, false
	}

	log.Printf("Skipping %s: interval not yet reached (%v (%v) <= %v)", check.name,
		int(age.Seconds()), age, check.RunInterval)
	if last.Status == nagiosCritical {
		deps.notOk(check.name)
	} else {
		deps.ok(check.name)
	}
	return checkResult{
		name:          check.name,
		output:        last.Output,
		epoch:         last.Epoch,
		status:        last.Status,
		federatedFrom: last.FederatedFrom,
	}, true
}

// runCheck waits for check's dependencies and runs it, retrying a non-OK
// result up to retries times. Every wait (random spread, retry interval)
// ends with ctx, so a run never outlives its deadline by sleeping; a retry
// that no longer fits keeps the last result.
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
		if err := sleepCtx(ctx, d); err != nil {
			deps.notOk(check.name)
			return check.skip("Run deadline reached before the check started")
		}
	}

	result := runCheckOnce(ctx, limitCh, check, conf)
	for ; result.status != nagiosOk && retries > 0; retries-- {
		retryDuration := time.Duration(check.RetryInterval) * time.Second
		if err := sleepCtx(ctx, retryDuration); err != nil {
			log.Printf("Not retrying %s: %v", check.name, err)
			break
		}
		log.Printf("Retrying %s after %v", check.name, retryDuration)
		result = runCheckOnce(ctx, limitCh, check, conf)
	}

	if result.status == nagiosCritical {
		deps.notOk(check.name)
	} else {
		deps.ok(check.name)
	}
	return result
}

// runCheckOnce runs check once within the CheckConcurrency limit and its
// CheckTimeoutS.
func runCheckOnce(ctx context.Context, limitCh chan struct{}, check namedCheck, conf config) checkResult {
	limitCh <- struct{}{}
	defer func() { <-limitCh }()

	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(conf.CheckTimeoutS)*time.Second)
	defer cancel()
	return check.run(checkCtx)
}

// sleepCtx sleeps for d or until ctx ends, returning ctx's error then.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
