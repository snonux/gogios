package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"
)

type checkState struct {
	status     int
	prevStatus int
	output     string
}

type state struct {
	stateFile string
	checks    map[string]checkState
	mutex     *sync.Mutex
}

func newState(config config) (state, error) {
	s := state{
		stateFile: fmt.Sprintf("%s/state.json", config.StateDir),
		checks:    make(map[string]checkState),
		mutex:     &sync.Mutex{},
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
	if err := json.Unmarshal(bytes, &s.checks); err != nil {
		return s, err
	}

	// Clean up obsolete state information
	var obsolete []string
	for name := range s.checks {
		if _, ok := config.Checks[name]; !ok {
			obsolete = append(obsolete, name)
		}
	}

	for _, name := range obsolete {
		delete(s.checks, name)
		log.Printf("State of %s is obsolete (removed)", name)
	}

	return s, nil
}

func (s state) update(name, output string, status int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	prevState, ok := s.checks[name]
	if !ok {
		log.Printf("State of %s: %d (new)", name, status)
		s.checks[name] = checkState{status, unknown, output}
		return
	}

	if prevState.status != status {
		log.Printf("State of %s: %d -> %d (changed)", name, prevState, status)
		s.checks[name] = checkState{status, prevState.status, output}
		return
	}

	log.Printf("State of %s: %d (unchanged)", name, status)
	s.checks[name] = checkState{status, prevState.status, output}
}

func (s state) persist() error {
	jsonData, err := json.Marshal(s.checks)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.stateFile, jsonData, os.ModePerm)
}

func (s state) report() (string, string, bool) {
	var sb strings.Builder
	var changed bool

	f := func(filter func(i int) bool) int {
		var count int
		for name, checkState := range s.checks {
			if filter(checkState.status) {
				count++
				if checkState.status != checkState.prevStatus {
					sb.WriteString(codeToString(checkState.prevStatus))
					sb.WriteString("->")
					changed = true
				}
				sb.WriteString(codeToString(checkState.status))
				sb.WriteString(": ")
				sb.WriteString(name)
				sb.WriteString(" ==>> ")
				sb.WriteString(checkState.output)
				sb.WriteString("\n")
			}
		}
		return count
	}

	sb.WriteString("This is the recent Googios report!\n\n")

	numCriticals := f(func(i int) bool { return i == 2 })
	if numCriticals > 0 {
		sb.WriteString("\n")
	}

	numWarnings := f(func(i int) bool { return i == 1 })
	if numWarnings > 0 {
		sb.WriteString("\n")
	}

	numUnknowns := f(func(i int) bool { return i > 2 })
	if numUnknowns > 0 {
		sb.WriteString("\n")
	}

	numOks := f(func(i int) bool { return i == 0 })
	if numOks > 0 {
		sb.WriteString("\n")
	}

	sb.WriteString("Have a nice day!\n")
	subject := fmt.Sprintf("GOGIOS Report [C:%d W:%d U:%d OK:%d]",
		numCriticals, numWarnings, numUnknowns, numOks)

	return subject, sb.String(), changed
}
