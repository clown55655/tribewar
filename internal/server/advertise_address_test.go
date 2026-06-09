package server

import "testing"

func TestAdvertiseAddressPrefersEnv(t *testing.T) {
	t.Setenv("TRIBEWAY_TEST_ADVERTISE_ADDRESS", "10.1.2.3")

	server := &BaseServer{}
	server.config = &ServerConfig{}
	server.config.Network.AdvertiseAddress = "192.168.1.10"
	server.config.Network.AdvertiseAddressEnv = "TRIBEWAY_TEST_ADVERTISE_ADDRESS"

	if got := server.advertiseAddress(); got != "10.1.2.3" {
		t.Fatalf("expected env advertise address, got %q", got)
	}
}

func TestAdvertiseAddressSkipsUnspecifiedAddress(t *testing.T) {
	server := &BaseServer{}
	server.config = &ServerConfig{}
	server.config.Network.AdvertiseAddress = "0.0.0.0"
	server.config.Network.AdvertiseAddressEnv = "TRIBEWAY_TEST_EMPTY_ADVERTISE_ADDRESS"

	got := server.advertiseAddress()
	if got == "" || got == "0.0.0.0" {
		t.Fatalf("expected concrete advertise address, got %q", got)
	}
}
