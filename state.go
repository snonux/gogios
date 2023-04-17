package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
)

type state struct {
	stateFile string
	Checks    map[string]int
	mutex     sync.Mutex
}

func newState(config config) (state, error) {
	s := state{
		stateFile: fmt.Sprintf("%s/state.json", config.StateDir),
		Checks:    make(map[string]int),
	}

	if _, err := os.Stat(s.stateFile); err != nil {
		// OK, may be first run with no state yet.
		return s, nil
	}

	file, err := os.Open(s.stateFile)
	if err != nil {
		return s, err
	}
	defer file.Close()

	// Read the file content
	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return s, err
	}

	// Parse the JSON content
	if err := json.Unmarshal(bytes, &s.Checks); err != nil {
		return s, err
	}

	// Clean up obsolete state information
	var obsolete []string
	for name := range s.Checks {
		if _, ok := config.Checks[name]; !ok {
			obsolete = append(obsolete, name)
		}
	}

	for _, name := range obsolete {
		delete(s.Checks, name)
		log.Printf("State of %s is obsolete (removed)", name)
	}

	return s, nil
}

func (s state) update(name string, status int) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	oldStatus, ok := s.Checks[name]
	if !ok {
		log.Printf("State of %s: %d (new)", name, status)
		s.Checks[name] = status
		return true
	}

	if oldStatus != status {
		log.Printf("State of %s: %d -> %d (changed)", name, oldStatus, status)
		s.Checks[name] = status
		return true
	}

	log.Printf("State of %s: %d (unchanged)", name, status)
	return false
}

func (s state) persist() error {
	jsonData, err := json.Marshal(s.Checks)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.stateFile, jsonData, os.ModePerm)
}
