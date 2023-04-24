package internal

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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
	Checks           map[string]check
}

func newConfig(configFile string) (config, error) {
	var config config

	file, err := os.Open(configFile)
	if err != nil {
		return config, err
	}
	defer file.Close()

	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return config, err
	}

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return config, err
	}

	if config.SMTPServer == "" {
		hostname, err := os.Hostname()
		if err != nil {
			panic(err)
		}
		config.SMTPServer = fmt.Sprintf("%s:25", hostname)
		log.Println("Set SMTPServer to " + config.SMTPServer)
	}

	if config.StateDir == "" {
		config.StateDir = "."
		log.Println("Set StateDir to " + config.StateDir)
	}

	return config, nil
}

func (c config) sanityCheck() error {
	for name, check := range c.Checks {
		for _, depName := range check.DependsOn {
			if _, ok := c.Checks[depName]; !ok {
				return fmt.Errorf("check '%s' depends on non existant check '%s'", depName, name)
			}
		}
	}
	return nil
}
