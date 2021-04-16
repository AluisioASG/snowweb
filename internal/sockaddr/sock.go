// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package sockaddr

import (
	"net"
)

// ListenerFromString parses a string in the form `network:address` and
// creates a listening socket bound to the given address.
//
// See the documentation of sockaddr.SplitNetworkAddress for details of
// the input string format, and net.Dial and sockaddr.FDName for the
// supported networks.
func ListenerFromString(s string) (net.Listener, error) {
	network, address, err := SplitNetworkAddress(s)
	if err != nil {
		return nil, err
	}

	switch network {
	case "fd", "systemd":
		f, err := FDFile(network, address)
		if err != nil {
			return nil, err
		}
		return net.FileListener(f)
	default:
		return net.Listen(network, address)
	}
}

// ConnFromString parses a string in the form `network:address` and
// creates a socket connected to the given address.
//
// See the documentation of sockaddr.SplitNetworkAddress for details of
// the input string format, and net.Dial and sockaddr.FDName for the
// supported networks.
func ConnFromString(s string) (net.Conn, error) {
	network, address, err := SplitNetworkAddress(s)
	if err != nil {
		return nil, err
	}

	switch network {
	case "fd", "systemd":
		f, err := FDFile(network, address)
		if err != nil {
			return nil, err
		}
		return net.FileConn(f)
	default:
		return net.Dial(network, address)
	}
}
