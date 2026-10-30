// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

// Package backend is the contract between the portable half of usbio and its
// one implementation per operating system -- usbfs for Linux, winusb for
// Windows, iokit for macOS: a way to list what is attached, and an opened
// device with the four kinds of transfer on it.
package backend

import "errors"

// ErrTimeout reports that a transfer expired before the device answered; a
// streaming read that returns it simply has no data yet.
var ErrTimeout = errors.New("USB transfer timeout")

// ErrUnsupported reports that this operating system has no direct USB backend.
var ErrUnsupported = errors.New("direct USB access is not supported on this operating system")

// ErrGone reports that the device is no longer attached. Unlike an ordinary
// transfer failure it cannot be retried: the caller must give the handle up.
var ErrGone = errors.New("USB device is no longer attached")

// ErrNoDevices reports that the USB tree itself could not be read -- no sysfs,
// no device nodes, no permission. Finding nothing attached is an empty list.
var ErrNoDevices = errors.New("the USB bus cannot be read")

// The pieces of chapter 9 of the USB specification that both halves need.
const (
	RequestGetDescriptor  = 0x06
	DescriptorTypeDevice  = 0x01
	DescriptorTypeConfig  = 0x02
	DescriptorTypeString  = 0x03
	DeviceDescriptorBytes = 18
)

// ID is a USB vendor/product pair. Each board driver lists the IDs it claims.
type ID struct {
	Vendor  uint16
	Product uint16
}

// In reports whether the identifier appears in a list.
func (id ID) In(list []ID) bool {
	for _, entry := range list {
		if id == entry {
			return true
		}
	}
	return false
}

// Found is one enumerated USB device, before anything is opened. Listing is kept
// separate from opening so an unwanted device is never touched: on Linux opening
// the node needs write permission the caller may not have.
type Found struct {
	ID ID
	// Location is the "bus:address" name a selector matches against. Windows
	// includes the full dotted port route for hubs, or an interface path when
	// the topology is unavailable.
	Location string
	// Serial is the device's serial-number string, when the operating system knew
	// it without the device being opened.
	Serial string
	// Port is the physical path from the root hub, "1-2.3". Empty where the
	// backend cannot say.
	Port string
	// Held names the driver the operating system has bound to the device instead
	// of one this program can claim through: a vendor's BDA driver on Windows, a
	// kernel driver on Linux. Empty when the device is free, and wherever the
	// backend cannot tell without opening it.
	Held string
	// Open claims interface 0 of this device.
	Open func() (Device, error)
	// OpenControlOnly opens the device without claiming an interface: enough for a
	// control transfer, and all that is left when a device is too stuck to be
	// configured. Nil where the backend has no such thing.
	OpenControlOnly func() (Device, error)
	// Reset puts the device through a bus reset, leaving it on the bus at the same
	// name. Nil where the backend has no such call.
	Reset func() error
	// Release drops whatever the listing held on to, for every candidate once Open
	// has made its choice; an opened device is independent of it. Nil when listing
	// holds nothing, as on Linux.
	Release func()
}

// Drop hands back whatever a listing held on to, for every candidate once the
// caller has made its choice.
func Drop(devices []Found) {
	for _, candidate := range devices {
		if candidate.Release != nil {
			candidate.Release()
		}
	}
}

// Device is an opened USB device with its interface claimed.
type Device interface {
	Control(requestType, request byte, value, index uint16, buffer []byte, timeoutMS uint32) (int, error)
	Bulk(endpoint byte, buffer []byte, timeoutMS uint32) (int, error)
	Isochronous(endpoint byte, buffer []byte, packetSize, packets int, timeoutMS uint32) (int, error)
	// OpenIsoStream starts an isochronous endpoint with several transfers kept
	// outstanding. A backend without one returns ErrUnsupported, and its caller
	// falls back to Isochronous above.
	OpenIsoStream(endpoint byte, packetSize, packets, depth int, timeoutMS uint32) (IsoStream, error)
	// OpenBulkStream starts reading a bulk endpoint with depth transfers of size
	// bytes kept outstanding. A backend without one returns ErrUnsupported, and
	// its caller falls back to Bulk above.
	OpenBulkStream(endpoint byte, size, depth int, timeoutMS uint32) (Stream, error)
	SetAlternate(interfaceNumber, alternate int) error
	Close() error
}

// Stream is a running endpoint with transfers kept outstanding: Read hands back
// what the oldest ones carried, packed to the front of the buffer, and puts them
// back in flight.
type Stream interface {
	Read(buffer []byte, timeoutMS uint32) (int, error)
	Close() error
}

// IsoStream is a running isochronous endpoint.
type IsoStream = Stream
