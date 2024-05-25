package dnsproxytest_test

import (
	"github.com/bruceluk/dnsproxy/internal/dnsproxytest"
	"github.com/bruceluk/dnsproxy/upstream"
)

// type check
var _ upstream.Upstream = (*dnsproxytest.FakeUpstream)(nil)
