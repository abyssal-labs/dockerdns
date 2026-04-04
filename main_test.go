package main

import (
	"net"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

func TestMatchContainerName(t *testing.T) {
	tests := []struct {
		query  string
		domain string
		want   string
		ok     bool
	}{
		{"sabnzbd.saltbox.local.", "saltbox.local", "sabnzbd", true},
		{"plex.saltbox.local", "saltbox.local", "plex", true},
		{"sab.nzbd.saltbox.local", "saltbox.local", "", false},
		{"sabnzbd.other.local", "saltbox.local", "", false},
	}

	for _, tt := range tests {
		got, ok := matchContainerName(tt.query, tt.domain)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("matchContainerName(%q, %q) = (%q, %v), want (%q, %v)", tt.query, tt.domain, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSelectIPsPrefersRequestedNetwork(t *testing.T) {
	inspect := container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {
					IPAddress: "172.17.0.5",
				},
				"saltbox": {
					IPAddress:         "172.19.0.5",
					GlobalIPv6Address: "fd00::5",
				},
			},
		},
	}

	ips := selectIPs(inspect, "saltbox")
	if len(ips) != 2 {
		t.Fatalf("expected 2 IPs, got %d", len(ips))
	}
	if !ips[0].Equal(net.ParseIP("172.19.0.5")) {
		t.Fatalf("expected IPv4 from preferred network, got %v", ips[0])
	}
	if !ips[1].Equal(net.ParseIP("fd00::5")) {
		t.Fatalf("expected IPv6 from preferred network, got %v", ips[1])
	}
}
