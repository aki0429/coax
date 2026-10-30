// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package winusb

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Till0196/coax/pkg/usbio/backend"
)

// describeDevice uses the full hub route as the selector. SPDRP_ADDRESS can
// be a hub-local port number, so it cannot identify devices on different hubs.
// Direct connections retain bus:port; nested ports add dotted path components.
func describeDevice(path, location string, hosts []string, service string) backend.Found {
	port := portPath(location, hosts)
	selector := path // Still unique when topology cannot be read.
	if bus, route, ok := strings.Cut(port, "-"); ok {
		components := append([]string{bus}, strings.Split(route, ".")...)
		valid := true
		for index, component := range components {
			number, err := strconv.Atoi(component)
			if err != nil || number < 0 {
				valid = false
				break
			}
			components[index] = fmt.Sprintf("%03d", number)
		}
		if valid {
			selector = components[0] + ":" + strings.Join(components[1:], ".")
		}
	}
	return backend.Found{
		Location: selector,
		Serial:   serialIn(path),
		Port:     port,
		Held:     heldBy(service),
	}
}

// heldBy names the driver a device is bound to when it is not WinUSB. The
// listing above takes in every device whatever its driver, and one bound to a
// vendor's BDA driver would otherwise be offered here as well as by the bda
// backend, and opening it would send WinUSB's requests to that driver. A device
// with no driver at all is not held: nothing else has it, and the open fails
// with the advice to bind it.
func heldBy(service string) string {
	if service == "" || strings.EqualFold(service, "WinUSB") {
		return ""
	}
	return service
}

// portPath turns a device's location into the "bus-port.port" shape this package
// names devices with, which is the shape Linux uses too.
func portPath(location string, hosts []string) string {
	at := strings.Index(location, "#USBROOT(")
	if at < 0 {
		return ""
	}
	bus := 0
	for index, host := range hosts {
		if strings.HasPrefix(location, host+"#") {
			bus = index + 1
			break
		}
	}
	if bus == 0 {
		return ""
	}
	var ports []string
	for _, part := range strings.Split(location[at:], "#") {
		if strings.HasPrefix(part, "USB(") && strings.HasSuffix(part, ")") {
			ports = append(ports, part[4:len(part)-1])
		}
	}
	if len(ports) == 0 {
		return ""
	}
	return fmt.Sprintf("%d-%s", bus, strings.Join(ports, "."))
}

// serialIn pulls the serial number out of a device interface path. Windows
// names a device instance after the serial its descriptor carries -- the "0" in
// \\?\usb#vid_0511&pid_0045#0#{a5dcbf10-...} -- so it is known without opening
// the device. A device with no serial gets an instance name Windows made up
// from where it is plugged in, and those always have an ampersand in them.
func serialIn(path string) string {
	parts := strings.Split(path, "#")
	if len(parts) < 4 || parts[2] == "" || strings.Contains(parts[2], "&") {
		return ""
	}
	return parts[2]
}
