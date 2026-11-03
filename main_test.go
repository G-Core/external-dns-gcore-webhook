package main

import (
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{name: "ipv4 loopback", host: "127.0.0.1", want: true},
		{name: "ipv4 loopback range", host: "127.0.0.5", want: true},
		{name: "ipv6 loopback", host: "::1", want: true},
		{name: "localhost name", host: "localhost", want: true},
		{name: "wildcard ipv4", host: "0.0.0.0", want: false},
		{name: "wildcard ipv6", host: "::", want: false},
		{name: "pod ip", host: "10.244.0.7", want: false},
		{name: "hostname", host: "webhook.internal", want: false},
		{name: "empty", host: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLoopbackHost(tc.host); got != tc.want {
				t.Errorf("isLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		port    string
		allow   bool
		want    string
		wantErr bool
	}{
		{name: "loopback ipv4", host: "127.0.0.1", port: "8888", want: "127.0.0.1:8888"},
		{name: "loopback ipv6", host: "::1", port: "8888", want: "[::1]:8888"},
		{name: "localhost name", host: "localhost", port: "8888", want: "localhost:8888"},
		{name: "wildcard refused", host: "0.0.0.0", port: "8888", wantErr: true},
		{name: "pod ip refused", host: "10.244.0.7", port: "8888", wantErr: true},
		{name: "wildcard allowed with opt-in", host: "0.0.0.0", port: "8888", allow: true, want: "0.0.0.0:8888"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.allow {
				t.Setenv(envAllowRemote, "true")
			}
			got, err := listenAddress(tc.host, tc.port)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("listenAddress(%q, %q) expected error, got addr %q", tc.host, tc.port, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("listenAddress(%q, %q) unexpected error: %v", tc.host, tc.port, err)
			}
			if got != tc.want {
				t.Errorf("listenAddress(%q, %q) = %q, want %q", tc.host, tc.port, got, tc.want)
			}
		})
	}
}

func TestResolveListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		port    string
		allow   bool
		want    string
		wantErr bool
	}{
		{name: "defaults to loopback", want: "127.0.0.1:8888"},
		{name: "explicit loopback", host: "127.0.0.1", port: "9000", want: "127.0.0.1:9000"},
		{name: "empty host falls back to loopback", host: "", port: "9000", want: "127.0.0.1:9000"},
		{name: "wildcard refused", host: "0.0.0.0", wantErr: true},
		{name: "wildcard allowed with opt-in", host: "0.0.0.0", port: "8888", allow: true, want: "0.0.0.0:8888"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envServerHost, tc.host)
			t.Setenv(envServerPort, tc.port)
			if tc.allow {
				t.Setenv(envAllowRemote, "true")
			}
			got, err := resolveListenAddress()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveListenAddress() expected error, got addr %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveListenAddress() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveListenAddress() = %q, want %q", got, tc.want)
			}
		})
	}
}
