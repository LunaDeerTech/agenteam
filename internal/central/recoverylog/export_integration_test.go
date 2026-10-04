//go:build integration

package recoverylog

import "io"

// IntegrationSink is compiled only into this package's integration test
// variant. Production builds expose no replaceable writer or admission hook.
func IntegrationSink(w interface {
	io.Writer
	Sync() error
	Close() error
}) *Sink {
	return newSink(w)
}
