package audit_test

import (
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/audit"
	"github.com/packratlabs/packrat-cli/internal/host"
	"github.com/packratlabs/packrat-cli/internal/rails"
)

var errNetwork = errors.New("the audit made an HTTP request")

type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.t.Errorf("HTTP request to %s", request.URL)
	return nil, errNetwork
}

// TestNoNetwork is the proof that the audit opens no socket without --host. A listener holds the
// one port the audit could dial and counts connections. The run with the probe is the control:
// it shows that the listener does count a dial. Do not weaken this test to make a change pass.
func TestNoNetwork(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = failingTransport{t}
	defer func() { http.DefaultTransport = previous }()

	listener, err := net.Listen("tcp", "127.0.0.1:8126")
	if err != nil {
		t.Skipf("port 8126 is in use on this machine, so its connections cannot be counted: %v", err)
	}
	var connections atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			if err := conn.Close(); err != nil {
				t.Errorf("closing a counted connection: %v", err)
			}
		}
	}()

	for _, fixture := range fixtures(t) {
		if _, err := audit.Run(filepath.Join("..", "..", "testdata", fixture), rails.AuditOptions()); err != nil {
			t.Fatalf("audit of %s: %v", fixture, err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if n := connections.Load(); n != 0 {
		t.Errorf("%d connections to port 8126 without --host", n)
	}

	opts := rails.AuditOptions()
	opts.Host = host.Default().Probe
	result, err := audit.Run(filepath.Join("..", "..", "testdata", "empty"), opts)
	if err != nil {
		t.Fatalf("audit with the probe: %v", err)
	}
	if result.Host == nil || !result.Host.TraceListener {
		t.Errorf("the probe did not find the listener: %+v", result.Host)
	}
	if err := listener.Close(); err != nil {
		t.Errorf("closing the listener: %v", err)
	}
	<-done
	if n := connections.Load(); n == 0 {
		t.Error("the control failed: the listener counted no connection from the probe")
	}
}
