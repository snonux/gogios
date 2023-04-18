package internal

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

type checkState struct {
	Status     nagiosCode
	PrevStatus nagiosCode
	output     string
}

type state struct {
	stateFile string
	checks    map[string]checkState
}

func readState(config config) (state, error) {
	s := state{
		stateFile: fmt.Sprintf("%s/state.json", config.StateDir),
		checks:    make(map[string]checkState),
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

	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return s, err
	}

	if err := json.Unmarshal(bytes, &s.checks); err != nil {
		return s, err
	}

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

func (s state) update(result checkResult) {
	prevStatus := unknown
	prevState, ok := s.checks[result.name]
	if ok {
		prevStatus = prevState.Status
	}

	checkState := checkState{result.status, prevStatus, result.output}
	s.checks[result.name] = checkState
	log.Println(result.name, checkState)
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

	f := func(filter func(n nagiosCode) bool) int {
		var count int
		for name, checkState := range s.checks {
			if !filter(checkState.Status) {
				continue
			}
			count++

			if checkState.Status != checkState.PrevStatus {
				sb.WriteString(nagiosCode(checkState.PrevStatus).Str())
				sb.WriteString("->")
				changed = true
			}

			sb.WriteString(nagiosCode(checkState.Status).Str())
			sb.WriteString(": ")
			sb.WriteString(name)
			sb.WriteString(" ==>> ")
			sb.WriteString(checkState.output)
			sb.WriteString("\n")
		}

		return count
	}

	sb.WriteString("This is the recent Gogios report!\n\n")

	numCriticals := f(func(n nagiosCode) bool { return n == 2 })
	if numCriticals > 0 {
		sb.WriteString("\n")
	}

	numWarnings := f(func(n nagiosCode) bool { return n == 1 })
	if numWarnings > 0 {
		sb.WriteString("\n")
	}

	numUnknowns := f(func(n nagiosCode) bool { return n > 2 })
	if numUnknowns > 0 {
		sb.WriteString("\n")
	}

	numOks := f(func(n nagiosCode) bool { return n == 0 })
	if numOks > 0 {
		sb.WriteString("\n")
	}

	sb.WriteString("Have a nice day!\n")
	subject := fmt.Sprintf("GOGIOS Report [C:%d W:%d U:%d OK:%d]",
		numCriticals, numWarnings, numUnknowns, numOks)

	return subject, sb.String(), changed
}
