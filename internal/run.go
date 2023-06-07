package internal

import "context"

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
		notify(conf, subject, body)
	}
}
