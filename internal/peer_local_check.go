package internal

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// peerLocalCheck is a check the active node synthesises (it is in no config)
// for the availability of the peer's host-local results. The peer's
// host-local checks themselves are notified only by the peer (see ownedBy),
// so when the peer's Gogios is unreachable or stuck, nobody would be told
// that its disk, load, ... are no longer watched. This check belongs to the
// active node and goes CRITICAL then, which mails.
const peerLocalCheck = "Gogios peer host checks"

// updatePeerLocalCheck records result as peerLocalCheck's new state.
func (s state) updatePeerLocalCheck(result checkResult) {
	result.name = peerLocalCheck
	s.update(result)
}

// peerFetchedResult is peerLocalCheck's result for a fetched peer report:
// OK while it is fresh, CRITICAL once it is older than staleAfter.
func peerFetchedResult(peer peerSnapshot, hostname string, now time.Time, staleAfter time.Duration) checkResult {
	hosts := peerLocalHosts(peer.Checks, hostname)
	if age := now.Sub(peer.LastUpdated); age > staleAfter {
		return checkResult{
			status: nagiosCritical,
			epoch:  now.Unix(),
			output: fmt.Sprintf("CRITICAL: peer report stale since %s (%v > %v); host-local checks of %s are not monitored",
				peer.LastUpdated.Format(time.RFC3339), age.Truncate(time.Second), staleAfter, hosts),
		}
	}
	return checkResult{
		status: nagiosOk,
		epoch:  now.Unix(),
		output: fmt.Sprintf("OK: host-local checks of %s merged from the peer report", hosts),
	}
}

// peerUnreachableResult is peerLocalCheck's result when the peer's report
// cannot be fetched. A single failure (a timeout, a restarting httpd) is only
// UNKNOWN, which does not mail; a failure in two runs in a row is CRITICAL.
func peerUnreachableResult(s state, conf config, fetchErr error, now time.Time) checkResult {
	status := nagiosUnknown
	if prev, ok := s.checks[peerLocalCheck]; ok && prev.Status != nagiosOk {
		status = nagiosCritical
	}
	return checkResult{
		status: status,
		epoch:  now.Unix(),
		output: fmt.Sprintf("%s: peer report %s unreachable (%v); its host-local checks are UNKNOWN",
			status.Str(), peerReportHost(conf.PeerURL), fetchErr),
	}
}

// peerLocalHosts names the hosts other than hostname (the peer mirrors this
// node's checks back) of the host-local checks in checks, or "the peer" when
// there are none.
func peerLocalHosts(checks map[string]checkState, hostname string) string {
	seen := make(map[string]bool)
	for _, cs := range checks {
		if cs.Host != "" && cs.Host != hostname {
			seen[cs.Host] = true
		}
	}
	if len(seen) == 0 {
		return "the peer"
	}
	hosts := make([]string, 0, len(seen))
	for host := range seen {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return strings.Join(hosts, ", ")
}

// peerReportHost returns the host of the peer's report URL, or the URL
// itself when it does not parse.
func peerReportHost(peerURL string) string {
	if u, err := url.Parse(peerURL); err == nil && u.Host != "" {
		return u.Host
	}
	return peerURL
}
