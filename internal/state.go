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

func (cs checkState) changed() bool {
	return cs.Status != cs.PrevStatus
}

type state struct {
	stateFile string
	checks    map[string]checkState
}

func readState(conf config) (state, error) {
	s := state{
		stateFile: fmt.Sprintf("%s/state.json", conf.StateDir),
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
		if _, ok := conf.Checks[name]; !ok {
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
	prevStatus := nagiosUnknown
	prevState, ok := s.checks[result.name]
	if ok {
		prevStatus = prevState.Status
	}

	cs := checkState{result.status, prevStatus, result.output}
	s.checks[result.name] = cs
	log.Println(result.name, cs)
}

func (s state) persist() error {
	jsonData, err := json.Marshal(s.checks)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.stateFile, jsonData, os.ModePerm)
}

func (s state) report(renotify bool) (string, string, bool) {
	var sb strings.Builder

	sb.WriteString("This is the recent Gogios report!\n\n")

	sb.WriteString("# Alerts with status changed:\n\n")
	changed := s.reportChanged(&sb)
	if !changed {
		sb.WriteString("There were no status changes...\n\n")
	}

	sb.WriteString("# Unhandled alerts:\n\n")
	numCriticals, numWarnings, numUnknown, numOK := s.reportUnhandled(&sb)
	hasUnhandled := (numCriticals + numWarnings + numUnknown) > 0
	if !hasUnhandled {
		sb.WriteString("There are no unhandled alerts...\n\n")
	}

	sb.WriteString("Have a nice day!\n")

	subject := fmt.Sprintf("GOGIOS Report [C:%d W:%d U:%d OK:%d]",
		numCriticals, numWarnings, numUnknown, numOK)

	return subject, sb.String(), changed || (renotify && hasUnhandled)
}

func (s state) reportChanged(sb *strings.Builder) (changed bool) {
	if 0 < s.reportBy(sb, true, func(cs checkState) bool {
		return cs.Status == nagiosCritical && cs.changed()
	}) {
		changed = true
	}

	if 0 < s.reportBy(sb, true, func(cs checkState) bool {
		return cs.Status == nagiosWarning && cs.changed()
	}) {
		changed = true
	}

	if 0 < s.reportBy(sb, true, func(cs checkState) bool {
		return cs.Status == nagiosUnknown && cs.changed()
	}) {
		changed = true
	}

	if 0 < s.reportBy(sb, true, func(cs checkState) bool {
		return cs.Status == nagiosOk && cs.changed()
	}) {
		changed = true
	}

	return
}

func (s state) reportUnhandled(sb *strings.Builder) (numCriticals, numWarnings,
	numUnknown, numOK int) {

	numCriticals = s.reportBy(sb, false, func(cs checkState) bool {
		return cs.Status == nagiosCritical
	})

	numWarnings = s.reportBy(sb, false, func(cs checkState) bool {
		return cs.Status == nagiosWarning
	})

	numUnknown = s.reportBy(sb, false, func(cs checkState) bool {
		return cs.Status == nagiosUnknown
	})

	numOK = s.countBy(func(cs checkState) bool {
		return cs.Status == nagiosOk
	})

	return
}

func (s state) reportBy(sb *strings.Builder, showStatusChange bool,
	filter func(cs checkState) bool) (count int) {

	for name, cs := range s.checks {
		if !filter(cs) {
			continue
		}
		count++

		if showStatusChange && cs.changed() {
			sb.WriteString(nagiosCode(cs.PrevStatus).Str())
			sb.WriteString("->")
		}

		sb.WriteString(nagiosCode(cs.Status).Str())
		sb.WriteString(": ")
		sb.WriteString(name)
		sb.WriteString(": ")
		sb.WriteString(cs.output)
		sb.WriteString("\n")
	}

	if count > 0 {
		sb.WriteString("\n")
	}
	return
}

func (s state) countBy(filter func(cs checkState) bool) (count int) {
	for _, cs := range s.checks {
		if filter(cs) {
			count++
		}
	}
	return
}
