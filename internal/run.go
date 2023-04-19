package internal

import "context"

func Run(cts context.Context, configFile string) {
	config, err := newConfig(configFile)
	if err != nil {
		panic(err)
	}

	state, err := readState(config)
	if err != nil {
		notifyError(config, err)
	}

	state = execute(ctx, state, config)

	if err := state.persist(); err != nil {
		notifyError(config, err)
	}

	if subject, body, doNotify := state.report(); doNotify {
		notify(config, subject, body)
	}
}
