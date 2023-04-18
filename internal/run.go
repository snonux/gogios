package internal

func Run(configFile string) {
	config, err := newConfig(configFile)
	if err != nil {
		panic(err)
	}

	state, err := readState(config)
	if err != nil {
		notifyError(config, err)
	}

	state = execute(state, config)

	if err := state.persist(); err != nil {
		notifyError(config, err)
	}

	if subject, body, changed := state.report(); changed {
		notify(config, subject, body)
	}
}
