// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package sockaddr

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	systemd "github.com/coreos/go-systemd/activation"
)

// systemdSocketFiles contains the file descriptors sent through
// systemd's socket activation protocol (see man:sd_listen_fds(3)).
var systemdSocketFiles = systemd.Files(true)

// ErrNoSystemdSockets is returned by FDFile when $LISTEN_PID does not
// match the current process or $LISTEN_FDS is 0, both meaning that no
// sockets were passed by systemd to this process.
var ErrNoSystemdSockets = errors.New("no sockets were passed by systemd to the current process")

// SplitNetworkAddress parses a string in the form `network:address`,
// which can then be passed to net.Dial and similar functions.
//
// If the string cannot be parsed, a ParseError is returned.
func SplitNetworkAddress(s string) (string, string, error) {
	split := strings.SplitN(s, ":", 2)
	if len(split) != 2 {
		return "", "", &ParseError{net.ParseError{Type: "socket address", Text: s}, nil}
	}

	return split[0], split[1], nil
}

// FDFile creates an os.File wrapping a file descriptor that can be
// passed to net.FileConn or net.FileListener.
//
// Known networks are "fd" and "systemd".  If an unknown network is
// given, net.UnknownNetworkError is returned.
//
// For "fd" networks, the address is simply the file descriptor number.
// For example, "2" is the address of the standard error stream.
//
// For "systemd" networks, the optional address is the name of a file
// descriptor passed by systemd (see sd_listen_fds_with_names(3)).
// If the address is empty, the first file descriptor passed in is used.
//
// If a "fd" network is given and the address cannot be parsed,
// a ParseError is returned.
//
// If a "systemd" network is given but no file descriptors were passed,
// ErrNoSystemdSockets is returned.
//
// If a "systemd" network is given and no file descriptor with the
// requested name was passed by systemd, a net.AddrError is returned.
func FDFile(network, address string) (*os.File, error) {
	var fd uint64
	var err error

	switch network {
	default:
		return nil, net.UnknownNetworkError(network)
	case "fd":
		fd, err = strconv.ParseUint(address, 10, 0)
		if err != nil {
			return nil, &ParseError{net.ParseError{Type: "file descriptor", Text: address}, err}
		}
		return os.NewFile(uintptr(fd), fmt.Sprintf("/dev/fd/%v", fd)), nil
	case "systemd":
		if len(systemdSocketFiles) == 0 {
			return nil, ErrNoSystemdSockets
		}

		if address == "" {
			return systemdSocketFiles[0], nil
		} else {
			for _, f := range systemdSocketFiles {
				if f.Name() == address {
					return f, nil
				}
			}
			return nil, &net.AddrError{Err: "systemd socket not found", Addr: address}
		}
	}
}

// ParseError is returned when parsing a socket address or part of it
// fails.
type ParseError struct {
	net.ParseError

	// The error wrapped by this one, if any.
	Cause error
}

func (e *ParseError) Unwrap() error {
	return e.Cause
}
