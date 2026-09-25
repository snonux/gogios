package internal

import (
	"fmt"
	"log"
	"maps"
	"time"
)

// Host-local checks (check.Local) inspect the node Gogios runs on (disk,
// load, swap, ...), so only that node can run them. With peer failover the
// passive node still runs its host-local checks and notifies for them; the
// active node merges the peer's host-local results from the peer's JSON
// report, so both reports list the host checks of both nodes. Each check is
// notified only by the node that ran it (see ownedBy).

// localOnly returns conf reduced to its host-local checks, which is what a
// passive node runs.
func (conf config) localOnly() config {
	local := conf
	local.Checks = make(map[string]check)
	for name, chk := range conf.Checks {
		if chk.Local {
			local.Checks[name] = chk
		}
	}
	return local
}

// tagLocal sets Host on the results of the configured checks: this node's
// hostname for host-local ones (the peer tells them apart by it in the JSON
// report), empty for the others. Host-local check names must be unique
// across the peers (e.g. carry the hostname), or one node's result would
// shadow the other's.
func (s state) tagLocal(conf config) {
	for name, chk := range conf.Checks {
		cs, ok := s.checks[name]
		if !ok {
			continue
		}
		cs.Host = ""
		if chk.Local {
			cs.Host = conf.hostname
		}
		s.checks[name] = cs
	}
}

// ownLocalChecks returns this node's host-local check states from s, taken
// from the persisted state before a passive node replaces it by the peer's.
func (s state) ownLocalChecks(conf config) map[string]checkState {
	own := make(map[string]checkState)
	for name, chk := range conf.Checks {
		if cs, ok := s.checks[name]; ok && chk.Local {
			own[name] = cs
		}
	}
	return own
}

// restoreOwnLocal puts a passive node's own host-local states back after
// mirroring. The copies the peer published are dropped: they are at least one
// run old, and would make the next result compare against a stale status
// (a transition could mail twice or never). Mirrored entries tagged with this
// host but no longer configured are dropped too.
func (s state) restoreOwnLocal(own map[string]checkState, hostname string) {
	for name, cs := range s.checks {
		if cs.Host == hostname {
			delete(s.checks, name)
		}
	}
	maps.Copy(s.checks, own)
}

// mergePeerLocal adds the peer's host-local checks to the active node's state
// so its reports show them as well. A check the local config defines is never
// overwritten. When the peer's report is older than staleAfter, its checks
// are marked UNKNOWN with their old epoch: the peer's Gogios stopped, so its
// last results no longer say anything about its host.
func mergePeerLocal(s state, peer peerSnapshot, conf config, now time.Time, staleAfter time.Duration) state {
	stale := now.Sub(peer.LastUpdated) > staleAfter
	for name, cs := range peer.Checks {
		if cs.Host == "" || cs.Host == conf.hostname {
			continue
		}
		if _, defined := conf.Checks[name]; defined {
			log.Printf("Not merging peer host-local check %s: defined locally", name)
			continue
		}
		if stale && cs.Status != nagiosUnknown {
			// PrevStatus UNKNOWN too: the state is rebuilt from the peer
			// every run, so a changed status would reappear in the
			// "status changed" section on each run.
			cs.PrevStatus = nagiosUnknown
			cs.Status = nagiosUnknown
			cs.Output = fmt.Sprintf("UNKNOWN: report of %s stale since %s; last result: %s",
				cs.Host, peer.LastUpdated.Format(time.RFC3339), cs.Output)
		}
		s.checks[name] = cs
	}
	return s
}

// ownedBy returns the checks this node notifies for. The active node owns
// everything except a peer's host-local checks; a passive node owns only its
// own host-local checks (the active peer notifies for the rest).
func (s state) ownedBy(hostname string, passive bool) state {
	owned := state{stateFile: s.stateFile, staleEpoch: s.staleEpoch, checks: make(map[string]checkState)}
	for name, cs := range s.checks {
		own := cs.Host == "" || cs.Host == hostname
		if passive {
			own = cs.Host != "" && cs.Host == hostname
		}
		if own {
			owned.checks[name] = cs
		}
	}
	return owned
}
