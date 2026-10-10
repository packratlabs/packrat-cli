// Package host probes the machine that runs the audit. It is the only code that opens a socket,
// and it runs only with --host.
package host

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

// Prober holds what one probe looks at. Tests replace the fields; Default is the real machine.
type Prober struct {
	Paths    []string
	Address  string
	Timeout  time.Duration
	EnvNames []string
	// IsSet reports whether an environment variable has a value. It never returns the value.
	IsSet func(name string) bool
}

// Default probes the places of section 4.9 of the plan.
func Default() Prober {
	paths := []string{"/etc/datadog-agent/datadog.yaml", "/opt/datadog-agent"}
	if runtime.GOOS == "windows" {
		paths = []string{`C:\ProgramData\Datadog\datadog.yaml`}
	}
	return Prober{
		Paths:    paths,
		Address:  "localhost:8126",
		Timeout:  200 * time.Millisecond,
		EnvNames: []string{"DD_AGENT_HOST", "OTEL_EXPORTER_OTLP_ENDPOINT"},
		IsSet:    func(name string) bool { return os.Getenv(name) != "" },
	}
}

// Probe reads the facts. It sends no byte: the dial only opens a connection and closes it.
func (p Prober) Probe() audit.Host {
	found := audit.Host{AgentPaths: []string{}, EnvSet: []string{}, Notes: []string{}}
	for _, path := range p.Paths {
		if _, err := os.Stat(path); err == nil {
			found.AgentPaths = append(found.AgentPaths, path)
		}
	}
	if conn, err := net.DialTimeout("tcp", p.Address, p.Timeout); err == nil {
		found.TraceListener = true
		if err := conn.Close(); err != nil {
			found.Notes = append(found.Notes, fmt.Sprintf("closing the connection to %s: %v", p.Address, err))
		}
	}
	for _, name := range p.EnvNames {
		if p.IsSet(name) {
			found.EnvSet = append(found.EnvSet, name)
		}
	}
	return found
}
