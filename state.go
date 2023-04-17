package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

type state struct {
	stateFile string
	Checks    map[string]int
}

func newState(config config) (state, error) {
	stateFile := fmt.Sprintf("%s/state.json", config.StateDir)
	state := state{stateFile, make(map[string]int)}

	if _, err := os.Stat(stateFile); err != nil {
		// OK, may be first run with no state yet.
		return state, nil
	}

	file, err := os.Open(stateFile)
	if err != nil {
		return state, err
	}
	defer file.Close()

	// Read the file content
	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return state, err
	}

	// Parse the JSON content
	if err := json.Unmarshal(bytes, &state.Checks); err != nil {
		return state, err
	}

	// Clean up obsolete state information
	var obsolete []string
	for name := range state.Checks {
		if _, ok := config.Checks[name]; !ok {
			obsolete = append(obsolete, name)
		}
	}

	for _, name := range obsolete {
		delete(state.Checks, name)
		log.Printf("State of %s is obsolete (removed)", name)
	}

	return state, nil
}

func (s state) update(name string, status int) bool {
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
