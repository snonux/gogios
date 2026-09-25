package internal

import "log"

// checksFromSections rebuilds a check state map from a peer's JSON report.
// The unhandled, stale, suppressed and OK sections together hold every check
// exactly once (see jsonReport); the status-changed section only adds the
// previous status of checks that changed in the peer's last run. It returns
// nil when the report lists no checks, e.g. one from an older Gogios.
func checksFromSections(sections jsonSections) map[string]checkState {
	checks := make(map[string]checkState)
	for _, section := range [][]jsonCheck{sections.Unhandled, sections.Stale, sections.Suppressed, sections.Ok} {
		for _, jc := range section {
			status := parseNagiosCode(jc.Status)
			checks[jc.Name] = checkState{
				Status:        status,
				PrevStatus:    status,
				Epoch:         jc.Epoch,
				Output:        jc.Output,
				FederatedFrom: jc.FederatedFrom,
				Host:          jc.Host,
				// The peer's Hidden is this copy's previous one, so the
				// passive node's markHidden keeps a hidden status hidden.
				Hidden:     jc.Hidden,
				PrevHidden: jc.Hidden,
			}
		}
	}
	if len(checks) == 0 {
		return nil
	}
	for _, jc := range sections.StatusChanged {
		if cs, ok := checks[jc.Name]; ok && jc.PrevStatus != "" {
			cs.PrevStatus = parseNagiosCode(jc.PrevStatus)
			checks[jc.Name] = cs
		}
	}
	return checks
}

// mirrorPeerState replaces the passive node's check state with the active
// peer's, so both frontends publish the same current report and a node that
// becomes active later starts from recent state instead of the one it froze
// when it went passive (which showed up as a diverging, stale report). With
// no checks in the snapshot, the local state is kept.
func mirrorPeerState(s state, peer peerSnapshot) state {
	if len(peer.Checks) == 0 {
		log.Println("Peer report has no checks to mirror; keeping local state")
		return s
	}
	s.checks = peer.Checks
	log.Printf("Mirrored %d checks from the active peer", len(peer.Checks))
	return s
}

// parseNagiosCode is the inverse of nagiosCode.Str; anything else is UNKNOWN.
func parseNagiosCode(str string) nagiosCode {
	switch str {
	case "OK":
		return nagiosOk
	case "WARNING":
		return nagiosWarning
	case "CRITICAL":
		return nagiosCritical
	default:
		return nagiosUnknown
	}
}
