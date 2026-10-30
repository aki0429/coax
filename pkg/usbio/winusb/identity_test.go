// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package winusb

import (
	"fmt"
	"testing"
)

func TestDevicesOnDifferentHubsHaveDistinctSelectors(t *testing.T) {
	const host = "PCIROOT(0)#PCI(1400)"
	// Three two-port hubs: the old bus:SPDRP_ADDRESS selector named these six
	// synthetic devices with just two values, 001:001 and 001:002.
	seen := make(map[string]bool)
	for hub := 1; hub <= 3; hub++ {
		for port := 1; port <= 2; port++ {
			path := fmt.Sprintf(`\\?\usb#vid_ffff&pid_ffff#6&abcdef&0&%d%d#{a5dcbf10-6530-11d2-901f-00c04fb951ed}`, hub, port)
			location := fmt.Sprintf("%s#USBROOT(0)#USB(%d)#USB(%d)", host, hub, port)
			for _, hosts := range [][]string{{host}, nil} {
				found := describeDevice(path, location, hosts, "WinUSB")
				want := path
				if hosts != nil {
					want = fmt.Sprintf("001:%03d.%03d", hub, port)
				}
				if found.Location != want {
					t.Fatalf("selector = %q, want %q", found.Location, want)
				}
				if hosts != nil && found.Port != fmt.Sprintf("1-%d.%d", hub, port) {
					t.Fatalf("lost full hub topology: %q", found.Port)
				}
				if found.Serial != "" || found.Held != "" {
					t.Fatalf("unexpected serial or driver: %+v", found)
				}
			}
			found := describeDevice(path, location, []string{host}, "WinUSB")
			if seen[found.Location] {
				t.Fatalf("different devices share selector %q", found.Location)
			}
			seen[found.Location] = true
		}
	}
}

func TestDirectDeviceRetainsBusAddressSelector(t *testing.T) {
	const host = "PCIROOT(0)#PCI(1400)"
	found := describeDevice(`\\?\usb#vid_ffff&pid_ffff#test-device#{test-interface}`, host+"#USBROOT(0)#USB(2)", []string{host}, "WinUSB")
	if found.Location != "001:002" {
		t.Fatalf("direct device selector = %q, want 001:002", found.Location)
	}
}

func TestSimilarPortNumbersHaveDistinctSelectors(t *testing.T) {
	const host = "PCIROOT(0)#PCI(1400)"
	seen := make(map[string]bool)
	for _, route := range []string{"USB(1)", "USB(10)", "USB(1)#USB(1)", "USB(1)#USB(10)", "USB(10)#USB(1)"} {
		found := describeDevice("test-interface", host+"#USBROOT(0)#"+route, []string{host}, "WinUSB")
		if seen[found.Location] {
			t.Fatalf("port routes alias at %q", found.Location)
		}
		seen[found.Location] = true
	}
}
