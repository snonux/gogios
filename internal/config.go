package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

type config struct {
	EmailTo          string
	EmailFrom        string
	SMTPServer       string `json:"SMTPServer,omitempty"`
	StateDir         string `json:"StateDir,omitempty"`
	CheckTimeoutS    int
	CheckConcurrency int
	StaleThreshold   int      `json:"StaleThreshold,omitempty"`
	Federated        []string `json:"Federated,omitempty"`
	Checks           map[string]check
}

func newConfig(configFile string) (config, error) {
	var conf config

	file, err := os.Open(configFile)
	if err != nil {
		return conf, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return conf, err
	}

	err = json.Unmarshal(bytes, &conf)
	if err != nil {
		return conf, err
	}

	if conf.SMTPServer == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Fatal(err)
		}
		conf.SMTPServer = fmt.Sprintf("%s:25", hostname)
		log.Println("Set SMTPServer to " + conf.SMTPServer)
	}

	if conf.StateDir == "" {
		conf.StateDir = "."
		log.Println("Set StateDir to " + conf.StateDir)
	}

	if conf.StaleThreshold == 0 {
		conf.StaleThreshold = 3600 // Default to 1 hour
	}

	return conf, nil
}

func (conf config) sanityCheck() error {
	for name, check := range conf.Checks {
		for _, depName := range check.DependsOn {
			if _, ok := conf.Checks[depName]; !ok {
				return fmt.Errorf("check '%s' depends on non existant check '%s'", name, depName)
			}
		}
	}
	return nil
}
