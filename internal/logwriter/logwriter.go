// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package logwriter

import (
	"errors"
	"io"
	"log/syslog"
	"os"

	"git.sr.ht/~aasg/snowweb/internal/sockaddr"
	systemdJournal "github.com/coreos/go-systemd/journal"
	"github.com/rs/zerolog"
	zerologJournald "github.com/rs/zerolog/journald"
)

// ErrJournaldUnavailable is returned when the systemd journal cannot
// be written to (e.g. because the system does not use systemd).
var ErrJournaldUnavailable = errors.New("cannot connect to systemd journal socket")

// Writer returns an io.Writer that writes to the specified location.
//
// Supported addresses are:
// - "stdout" or "stderr", to write to one of the standard streams;
// - "journald", to write directly to the systemd journal,
// - any address supported by sockaddr.SplitNetworkAddress, to write to
//   a syslog daemon.
func Writer(address string) (io.Writer, error) {
	switch address {
	case "stderr":
		return zerolog.ConsoleWriter{Out: os.Stderr}, nil
	case "stdout":
		return zerolog.ConsoleWriter{Out: os.Stdout}, nil
	case "journald":
		if !systemdJournal.Enabled() {
			return nil, ErrJournaldUnavailable
		}
		return zerologJournald.NewJournalDWriter(), nil
	default:
		network, address, err := sockaddr.SplitNetworkAddress(address)
		if err != nil {
			return nil, err
		}

		syslogWriter, err := syslog.Dial(network, address, syslog.LOG_DAEMON, "snowweb")
		if err != nil {
			return nil, err
		}

		return zerolog.SyslogCEEWriter(syslogWriter), nil
	}
}
