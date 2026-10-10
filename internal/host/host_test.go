package host_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/packratlabs/packrat-cli/internal/host"
)

const marker = "HOST_VALUE_DO_NOT_PRINT"

func prober(paths []string, address string) host.Prober {
	return host.Prober{
		Paths:    paths,
		Address:  address,
		Timeout:  200 * time.Millisecond,
		EnvNames: []string{"DD_AGENT_HOST", "OTEL_EXPORTER_OTLP_ENDPOINT"},
		IsSet:    func(name string) bool { return name == "DD_AGENT_HOST" },
	}
}

func TestProbeFindsAgentFileListenerAndEnvName(t *testing.T) {
	config := filepath.Join(t.TempDir(), "datadog.yaml")
	if err := os.WriteFile(config, []byte("api_key: "+marker+"\n"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", config, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("closing the listener: %v", err)
		}
	}()
	absent := filepath.Join(t.TempDir(), "absent")
	got := prober([]string{absent, config}, listener.Addr().String()).Probe()
	if want := []string{config}; !reflect.DeepEqual(got.AgentPaths, want) {
		t.Errorf("agent paths %v, want %v", got.AgentPaths, want)
	}
	if !got.TraceListener {
		t.Error("the listener was not found")
	}
	if want := []string{"DD_AGENT_HOST"}; !reflect.DeepEqual(got.EnvSet, want) {
		t.Errorf("env set %v, want %v", got.EnvSet, want)
	}
	if len(got.Notes) != 0 {
		t.Errorf("notes %v, want none", got.Notes)
	}
}

func TestProbeOfAMachineWithNothing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	closed := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("closing the listener: %v", err)
	}
	p := prober([]string{filepath.Join(t.TempDir(), "absent")}, closed)
	p.IsSet = func(string) bool { return false }
	got := p.Probe()
	if got.AgentPaths == nil || got.EnvSet == nil || got.Notes == nil {
		t.Errorf("a list is nil, and would print as null: %+v", got)
	}
	if len(got.AgentPaths) != 0 || got.TraceListener || len(got.EnvSet) != 0 {
		t.Errorf("found something on a machine with nothing: %+v", got)
	}
}

// The default probe must not keep the value of a variable, only that it is set.
func TestDefaultKeepsNamesNotValues(t *testing.T) {
	t.Setenv("DD_AGENT_HOST", marker)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	p := host.Default()
	p.Address = "127.0.0.1:1"
	got := p.Probe()
	if want := []string{"DD_AGENT_HOST"}; !reflect.DeepEqual(got.EnvSet, want) {
		t.Errorf("env set %v, want %v", got.EnvSet, want)
	}
	if printed := fmt.Sprintf("%+v", got); strings.Contains(printed, marker) {
		t.Errorf("the probe kept a value: %s", printed)
	}
}
