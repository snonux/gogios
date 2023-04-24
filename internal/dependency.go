package internal

import (
	"context"
	"fmt"
)

type dependency struct {
	okMap  map[string]chan struct{}
	nokMap map[string]chan struct{}
}

func newDependency(config config) dependency {
	d := dependency{
		okMap:  make(map[string]chan struct{}, len(config.Checks)),
		nokMap: make(map[string]chan struct{}, len(config.Checks)),
	}

	for name := range config.Checks {
		d.okMap[name] = make(chan struct{})
		d.nokMap[name] = make(chan struct{})
	}

	return d
}

func (d dependency) ok(name string) {
	close(d.okMap[name])
}

func (d dependency) notOk(name string) {
	close(d.nokMap[name])
}

// Wait for all dependant checks to be executed!
func (d dependency) wait(ctx context.Context, dependencies []string) error {
	for _, dep := range dependencies {
		if _, ok := d.okMap[dep]; !ok {
			// We sent an error mail already via config.sanityCheck for this case.
			continue
		}
		select {
		case <-d.okMap[dep]:
		case <-d.nokMap[dep]:
			return fmt.Errorf("dependency '%s' is not OK!", dep)
		case <-ctx.Done():
			return fmt.Errorf("waited for too long for dependency '%s': %s", dep, ctx.Err().Error())
		}
	}
	return nil
}
