package internal

import (
	"context"
	"log"
)

func Run(ctx context.Context, configFile string, renotify, force bool) {
	conf, err := newConfig(configFile)
	if err != nil {
		log.Fatal(err)
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

	if subject, body, doNotify := state.report(renotify, force); doNotify {
		if err := notify(conf, subject, body); err != nil {
			log.Println("error:", err)
		}
	}
}
