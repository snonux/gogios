package internal

import (
	"context"
	"fmt"
	"log"
	"os"
)

func Run(ctx context.Context, configFile string, renotify, force bool) {
	conf, err := newConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}

	if err := conf.sanityCheck(); err != nil {
		notifyError(conf, err)
	}

	state, err := newState(conf)
	if err != nil {
		notifyError(conf, err)
	}

	state = runChecks(ctx, state, conf)

	if err := state.persist(); err != nil {
		notifyError(conf, err)
	}

	subject, body, doNotify := state.report(renotify, force)
	if doNotify {
		if err := notify(conf, subject, body); err != nil {
			log.Println("error:", err)
			return
		}
	}
	if err := persistReport(body, conf); err != nil {
		notifyError(conf, err)
	}
}

func persistReport(body string, conf config) error {
	reportFile := fmt.Sprintf("%s/report.txt", conf.StateDir)
	tmpFile := fmt.Sprintf("%s.tmp", reportFile)

	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err = f.WriteString(body); err != nil {
		return err
	}
	return os.Rename(tmpFile, reportFile)
}
