package monitoring

import "testing"

func TestNormalizeMonitoringOptionsDefaultsToLoopback(t *testing.T) {
	options := normalizeMonitoringOptions(MonitoringOptions{})

	if options.BindAddress != "127.0.0.1" {
		t.Fatalf("expected loopback bind address, got %q", options.BindAddress)
	}
	if options.AdminTokenEnv != "TRIBEWAY_MONITORING_ADMIN_TOKEN" {
		t.Fatalf("unexpected token env: %q", options.AdminTokenEnv)
	}
	if len(options.AllowedCIDRs) == 0 {
		t.Fatal("expected default allowed CIDRs")
	}
}

func TestRemoteAddrAllowed(t *testing.T) {
	manager := &MonitoringManager{
		options: normalizeMonitoringOptions(MonitoringOptions{
			AllowedCIDRs: []string{"10.0.0.0/8"},
		}),
	}

	if !manager.remoteAddrAllowed("10.1.2.3") {
		t.Fatal("expected 10.1.2.3 to be allowed")
	}
	if manager.remoteAddrAllowed("192.168.1.10") {
		t.Fatal("expected 192.168.1.10 to be denied")
	}
}
