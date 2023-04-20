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

	// Open the file
	file, err := os.Open(configFile)
	if err != nil {
		return config, err
	}
	defer file.Close()

	// Read the file content
	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return config, err
	}

	// Parse the JSON content
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
