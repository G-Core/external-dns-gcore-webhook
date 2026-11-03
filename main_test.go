/*
Copyright 2017 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

func TestValidateListenHost(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		allowValue string
		wantErr    bool
	}{
		{name: "loopback ipv4", host: "127.0.0.1", allowValue: ""},
		{name: "loopback ipv6", host: "::1", allowValue: ""},
		{name: "localhost name", host: "localhost", allowValue: ""},
		{name: "wildcard refused", host: "0.0.0.0", allowValue: "", wantErr: true},
		{name: "pod ip refused", host: "10.244.0.7", allowValue: "", wantErr: true},
		{name: "wildcard allowed with opt-in true", host: "0.0.0.0", allowValue: "true"},
		{name: "wildcard allowed with opt-in 1", host: "0.0.0.0", allowValue: "1"},
		{name: "wildcard refused with opt-in false", host: "0.0.0.0", allowValue: "false", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set the opt-in for every case so the test does not depend on the
			// ambient environment of the machine running it.
			t.Setenv(envAllowRemote, tc.allowValue)
			err := validateListenHost(tc.host)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateListenHost(%q) expected error", tc.host)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateListenHost(%q) unexpected error: %v", tc.host, err)
			}
		})
	}
}

func TestBuildListenAddress(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{name: "ipv4 loopback", host: "127.0.0.1", port: "8888", want: "127.0.0.1:8888"},
		{name: "ipv6 loopback", host: "::1", port: "8888", want: "[::1]:8888"},
		{name: "localhost name", host: "localhost", port: "8888", want: "localhost:8888"},
		{name: "wildcard", host: "0.0.0.0", port: "8888", want: "0.0.0.0:8888"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildListenAddress(tc.host, tc.port); got != tc.want {
				t.Errorf("buildListenAddress(%q, %q) = %q, want %q", tc.host, tc.port, got, tc.want)
			}
		})
	}
}

func TestResolveListenAddress(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		port       string
		allowValue string
		want       string
		wantErr    bool
	}{
		{name: "defaults to loopback", allowValue: "", want: "127.0.0.1:8888"},
		{name: "explicit loopback", host: "127.0.0.1", port: "9000", allowValue: "", want: "127.0.0.1:9000"},
		{name: "empty host falls back to loopback", host: "", port: "9000", allowValue: "", want: "127.0.0.1:9000"},
		{name: "wildcard refused", host: "0.0.0.0", allowValue: "", wantErr: true},
		{name: "wildcard allowed with opt-in", host: "0.0.0.0", port: "8888", allowValue: "true", want: "0.0.0.0:8888"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envServerHost, tc.host)
			t.Setenv(envServerPort, tc.port)
			t.Setenv(envAllowRemote, tc.allowValue)
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
