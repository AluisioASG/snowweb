// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package sockaddr

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

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
// If the address is empty, the first file descriptor (given in the
// SD_LISTEN_FDS_START environment variable) is used.
//
// If a "fd" network is given and the address cannot be parsed,
// a ParseError is returned.
//
// If a "systemd" network is given and any of the requisite environment
// variables is missing (e.g.  because the program is not running under
// systemd), a SystemdEnvironmentError is returned.
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
	case "systemd":
		sdListenFDsStart, ok := os.LookupEnv("SD_LISTEN_FDS_START")
		if !ok {
			return nil, &SystemdEnvironmentError{Name: "SD_LISTEN_FDS_START"}
		}
		fd, err = strconv.ParseUint(sdListenFDsStart, 10, 0)
		if err != nil {
			return nil, &SystemdEnvironmentError{Name: "SD_LISTEN_FDS_START", Value: sdListenFDsStart, Cause: err}
		}

		if address != "" {
			listenFDNames, ok := os.LookupEnv("LISTEN_FDNAMES")
			if !ok {
				return nil, &SystemdEnvironmentError{Name: "LISTEN_FDNAMES"}
			}
			names := strings.Split(listenFDNames, ":")

			found := false
			for i, name := range names {
				if name == address {
					found = true
					fd += uint64(i)
					break
				}
			}
			if !found {
				return nil, &net.AddrError{Err: "named file descriptor not found", Addr: address}
			}
		}
	}

	return os.NewFile(uintptr(fd), fmt.Sprintf("/dev/fd/%v", fd)), nil
}

// SystemdEnvironmentError is returned by FDFile when working with a
// "systemd" network but an environment variable set by systemd is
// missing or cannot be parsed.
type SystemdEnvironmentError struct {
	// Name of the missing or unparsable environment variable.
	Name string
	// Value of the environment variable.
	Value string
	// The error wrapped by this one, if any.
	Cause error
}

func (e *SystemdEnvironmentError) Error() string {
	return fmt.Sprintf("sockaddr: missing or invalid environment variable %v: %q", e.Name, e.Value)
}

func (e *SystemdEnvironmentError) Unwrap() error {
	return e.Cause
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
