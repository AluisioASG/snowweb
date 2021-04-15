// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package listeners

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// FromString parses a string in the form `network:address` and
// creates a listening socket bound to the given address.
//
// Besides the network and address forms supported by net.Dial,
// the following formats are accepted:
// - `fd:$num` to create a socket from file descriptor `$num`
// - `systemd:$name` to use a named file descriptor passed by systemd
// - `systemd:` (no address) to use the first file descriptor passed
//   by systemd, regardless of name
func FromString(s string) (net.Listener, error) {
	split := strings.SplitN(s, ":", 2)
	if len(split) != 2 {
		return nil, fmt.Errorf("listeners: no separator between network and address")
	}

	network, address := split[0], split[1]
	switch network {
	default:
		return net.Listen(network, address)
	case "systemd":
		fd, err := strconv.ParseUint(os.Getenv("SD_LISTEN_FDS_START"), 10, 0)
		if err != nil {
			return nil, fmt.Errorf("listeners: could not parse SD_LISTEN_FDS_START: %w", err)
		}
		if address != "" {
			names := strings.Split(os.Getenv("LISTEN_FDNAMES"), ":")
			for i, name := range names {
				if name == address {
					fd += uint64(i)
					break
				}
			}
		}
		f := os.NewFile(uintptr(fd), s)
		return net.FileListener(f)
	case "fd":
		fd, err := strconv.ParseUint(address, 10, 0)
		if err != nil {
			return nil, fmt.Errorf("listeners: could not parse file descriptor number: %w", err)
		}
		f := os.NewFile(uintptr(fd), s)
		return net.FileListener(f)
	}
}
