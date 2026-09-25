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

// isPeerLocal reports whether the check name/cs is a peer's host-local check
// as merged into this node's state: tagged with another host and not
// defined in this node's config.
func (conf config) isPeerLocal(name string, cs checkState) bool {
	if cs.Host == "" || cs.Host == conf.hostname {
		return false
	}
	_, defined := conf.Checks[name]
	return !defined
}

// mergePeer folds the outcome of fetching the peer's report into the active
// node's state: the peer's host-local checks (see mergePeerLocal), or, when
// the peer could not be fetched, the last known ones turned UNKNOWN (see
// markPeerLocalUnreachable). Either way it updates peerLocalCheck, the check
// this node notifies for on the peer's behalf.
func mergePeer(s state, peer peerSnapshot, fetchErr error, conf config, now time.Time, staleAfter time.Duration) state {
	if fetchErr != nil {
		log.Println("Not merging peer host-local checks:", fetchErr)
		s.markPeerLocalUnreachable(conf, fetchErr)
		s.updatePeerLocalCheck(peerUnreachableResult(s, conf, fetchErr, now))
		return s
	}
	s = mergePeerLocal(s, peer, conf, now, staleAfter)
	s.updatePeerLocalCheck(peerFetchedResult(peer, conf.hostname, now, staleAfter))
	return s
}

// mergePeerLocal adds the peer's host-local checks to the active node's state
// so its reports show them as well, replacing the previous run's copies (a
// check the peer no longer reports disappears). A check the local config
// defines is never overwritten. When the peer's report is older than
// staleAfter, its checks are marked UNKNOWN with their old epoch: the peer's
// Gogios stopped, so its last results no longer say anything about its host.
func mergePeerLocal(s state, peer peerSnapshot, conf config, now time.Time, staleAfter time.Duration) state {
	for name, cs := range s.checks {
		if conf.isPeerLocal(name, cs) {
			delete(s.checks, name)
		}
	}
	stale := now.Sub(peer.LastUpdated) > staleAfter
	for name, cs := range peer.Checks {
		if cs.Host == "" || cs.Host == conf.hostname {
			continue
		}
		if _, defined := conf.Checks[name]; defined {
			log.Printf("Not merging peer host-local check %s: defined locally", name)
			continue
		}
		if stale {
			cs = unknownPeerLocal(cs, fmt.Sprintf("report of %s stale since %s",
				cs.Host, peer.LastUpdated.Format(time.RFC3339)))
		}
		s.checks[name] = cs
	}
	return s
}

// markPeerLocalUnreachable turns the peer's host-local checks kept from the
// previous run (newState keeps them) UNKNOWN when the peer's report cannot be
// fetched, instead of dropping them: a vanished check reads as fine, while
// the peer may just as well be down with a full disk.
func (s state) markPeerLocalUnreachable(conf config, fetchErr error) {
	for name, cs := range s.checks {
		if conf.isPeerLocal(name, cs) {
			s.checks[name] = unknownPeerLocal(cs, fmt.Sprintf("report of %s unreachable (%v)", cs.Host, fetchErr))
		}
	}
}

// unknownPeerLocal returns cs as UNKNOWN because of why, keeping its epoch
// and last result. PrevStatus becomes UNKNOWN too: the copy is refreshed from
// the peer on every run, so a changed status would reappear in the "status
// changed" section each time. An UNKNOWN copy is left alone so the message
// does not nest run after run.
func unknownPeerLocal(cs checkState, why string) checkState {
	if cs.Status == nagiosUnknown {
		return cs
	}
	cs.PrevStatus = nagiosUnknown
	cs.Status = nagiosUnknown
	cs.Output = fmt.Sprintf("UNKNOWN: %s; last result: %s", why, cs.Output)
	return cs
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
