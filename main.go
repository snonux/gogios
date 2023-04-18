package main

import (
	"flag"
)

func main() {
	configFile := flag.String("cfg", "/etc/gogios.json", "The config file")
	flag.Parse()

	config, err := newConfig(*configFile)
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
