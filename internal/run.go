package internal

import (
	"context"
	"log"
)

func Run(ctx context.Context, configFile string, renotify bool) {
	conf, err := newConfig(configFile)
	if err != nil {
		panic(err)
	}

	if err := conf.sanityCheck(); err != nil {
		notifyError(conf, err)
	}

	state, err := readState(conf)
	if err != nil {
		notifyError(conf, err)
	}

	state = runChecks(ctx, state, conf)

	if err := state.persist(); err != nil {
		notifyError(conf, err)
	}

	if subject, body, doNotify := state.report(renotify); doNotify {
		if err := notify(conf, subject, body); err != nil {
			log.Println("error:", err)
		}
	}
}
