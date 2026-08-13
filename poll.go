// Polling helper for daemon/service wait loops, plus the wait timings the
// platform installers share.

package main

import "time"

const (
	servicePollS = 90 * time.Second
	serviceTick  = 2 * time.Second
)

// pollUntil blocks until cond() returns true or the timeout elapses.
func pollUntil(cond func() bool, tick, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(tick)
	}
	return cond()
}
