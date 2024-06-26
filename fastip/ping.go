package fastip

import (
	"net/netip"
	"time"

	"github.com/AdguardTeam/golibs/log"
)

// pingTCPTimeout is a TCP connection timeout.  It's higher than pingWaitTimeout
// since the slower connections will be cached anyway.
const pingTCPTimeout = 4 * time.Second

// pingResult is the result of dialing the address.
type pingResult struct {
	// addrPort is the address-port pair the result is related to.
	addrPort netip.AddrPort

	// latency is the duration of dialing process in milliseconds.
	latency uint

	// success is true when the dialing succeeded.
	success bool
}

// schedulePings returns the result with the fastest IP address from the cache,
// if it's found, and starts pinging other IPs which are not cached or outdated.
// Returns scheduled flag which indicates that some goroutines have been
// scheduled.
func (f *FastestAddr) schedulePings(
	resCh chan *pingResult,
	ips []netip.Addr,
	host string,
) (pr *pingResult, scheduled bool) {
	for _, ip := range ips {
		cached := f.cacheFind(ip)
		if cached == nil {
			scheduled = true
			for _, port := range f.pingPorts {
				go f.pingDoTCP(host, netip.AddrPortFrom(ip, uint16(port)), resCh)
			}

			continue
		}

		if cached.status == 0 && (pr == nil || cached.latencyMsec < pr.latency) {
			pr = &pingResult{
				addrPort: netip.AddrPortFrom(ip, 0),
				latency:  cached.latencyMsec,
				success:  true,
			}
		}
	}

	return pr, scheduled
}

// pingAll pings all ips concurrently and returns as soon as the fastest one is
// found or the timeout is exceeded.
func (f *FastestAddr) pingAll(host string, ips []netip.Addr) (pr *pingResult) {
	ipN := len(ips)
	switch ipN {
	case 0:
		return nil
	case 1:
		return &pingResult{
			addrPort: netip.AddrPortFrom(ips[0], 0),
			success:  true,
		}
	}

	resCh := make(chan *pingResult, ipN*len(f.pingPorts))
	pr, scheduled := f.schedulePings(resCh, ips, host)
	if !scheduled {
		if pr != nil {
			log.Debug("fastip: pingAll: %s: return cached response: %s", host, pr.addrPort)
		} else {
			log.Debug("fastip: pingAll: %s: returning nothing", host)
		}

		return pr
	}

	res := f.firstSuccessRes(resCh, host)
	if res == nil {
		// In case of timeout return cached or nil.
		return pr
	}

	if pr == nil || res.latency <= pr.latency {
		// Cache wasn't found or is worse than res.
		return res
	}

	// Return cached result.
	return pr
}

// firstSuccessRes waits and returns the first successful ping result or nil in
// case of timeout.
func (f *FastestAddr) firstSuccessRes(resCh chan *pingResult, host string) (res *pingResult) {
	after := time.After(f.PingWaitTimeout)
	for {
		select {
		case res = <-resCh:
			log.Debug(
				"fastip: pingAll: %s: got result for %s status %v",
				host,
				res.addrPort,
				res.success,
			)

			if !res.success {
				continue
			}

			return res
		case <-after:
			log.Debug("fastip: pingAll: %s: pinging timed out", host)

			return nil
		}
	}
}

// pingDoTCP sends the result of dialing the specified address into resCh.
func (f *FastestAddr) pingDoTCP(host string, addrPort netip.AddrPort, resCh chan *pingResult) {
	log.Debug("pingDoTCP: %s: connecting to %s", host, addrPort)

	start := time.Now()
	conn, err := f.pinger.Dial("tcp", addrPort.String())
	elapsed := time.Since(start)

	success := err == nil
	if success {
		if cErr := conn.Close(); cErr != nil {
			log.Debug("fastip: closing tcp connection: %s", cErr)
		}
	}

	latency := uint(elapsed.Milliseconds())

	resCh <- &pingResult{
		addrPort: addrPort,
		latency:  latency,
		success:  success,
	}

	addr := addrPort.Addr().Unmap()
	if success {
		log.Debug("fastip: pingDoTCP: %s: elapsed %s ms on %s", host, elapsed, addrPort)
		f.cacheAddSuccessful(addr, latency)
	} else {
		log.Debug(
			"fastip: pingDoTCP: %s: failed to connect to %s, elapsed %s ms: %v",
			host,
			addrPort,
			elapsed,
			err,
		)
		f.cacheAddFailure(addr)
	}
}
