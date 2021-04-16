// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"log/syslog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"git.sr.ht/~aasg/snowweb"
	"git.sr.ht/~aasg/snowweb/internal/sockaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sean-/sysexits"
	"github.com/tywkeene/go-fsevents"
	"golang.org/x/sys/unix"
)

var listenAddress = flag.String("listen", "tcp:[::1]:", "TCP or Unix socket address to listen at")
var syslogAddress = flag.String("syslog", "", "TCP or Unix socket address of the syslog socket. If empty, messages are written to the console.")
var tlsCertificate = flag.String("certificate", "", "Path to TLS certificate")
var tlsKey = flag.String("key", "", "Path to TLS key")
var tlsClientCA = flag.String("client-ca", "", "Path to TLS client CA bundle")

var tlsKeyPair struct {
	mu          sync.RWMutex
	certificate *tls.Certificate
}

func main() {
	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	if *syslogAddress != "" {
		network, address, err := sockaddr.SplitNetworkAddress(*syslogAddress)
		if err != nil {
			log.Error().Err(err).Str("address", *syslogAddress).Msg("could not parse syslog address")
			os.Exit(sysexits.Usage)
		}

		syslogWriter, err := syslog.Dial(network, address, syslog.LOG_DAEMON, "snowweb")
		if err != nil {
			log.Error().Err(err).Str("address", *syslogAddress).Msg("could not connect to syslog daemon")
			os.Exit(sysexits.Unavailable)
		}
		log.Logger = log.Output(zerolog.SyslogCEEWriter(syslogWriter))
	}

	if flag.NArg() != 1 {
		log.Error().Msg("no installable given in the command line")
		os.Exit(sysexits.Usage)
	}
	installable := flag.Arg(0)

	listener, err := sockaddr.ListenerFromString(*listenAddress)
	if err != nil {
		log.Error().Err(err).Str("address", *listenAddress).Msg("could not create listening socket")
		os.Exit(sysexits.Unavailable)
	}

	if (*tlsCertificate == "") != (*tlsKey == "") {
		log.Error().Msg("no TLS certificate or TLS key given in the command line")
		os.Exit(sysexits.Usage)
	}
	enableTLS := *tlsCertificate != ""
	if enableTLS {
		if err := loadTLSKeyPair(); err != nil {
			log.Error().Err(err).Msg("could not load TLS keypair")
			os.Exit(sysexits.NoInput)
		}

		tlsConfig := tls.Config{
			GetCertificate:           getCertificate,
			MinVersion:               tls.VersionTLS12,
			NextProtos:               []string{"h2", "http/1.1"},
			PreferServerCipherSuites: true,
			SessionTicketsDisabled:   true,
		}

		if *tlsClientCA != "" {
			pem, err := os.ReadFile(*tlsClientCA)
			if err != nil {
				log.Error().Err(err).Str("path", *tlsClientCA).Msg("could not read client CA bundle")
				os.Exit(sysexits.NoInput)
			}

			clientCAPool := x509.NewCertPool()
			if !clientCAPool.AppendCertsFromPEM(pem) {
				log.Error().Err(err).Str("path", *tlsClientCA).Msg("could not parse certificates from client CA bundle")
				os.Exit(sysexits.DataErr)
			}

			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
			tlsConfig.ClientCAs = clientCAPool
			log.Debug().Str("ca_path", *tlsClientCA).Msg("enabled client certificate verification")
		}

		listener = tls.NewListener(listener, &tlsConfig)
		log.Debug().Msg("TLS and HTTP/2 enabled")
	}

	// Create the handler and perform the initial build.
	siteHandler := snowweb.NewSnowWebServer(installable)
	if err := siteHandler.Realise(); err != nil {
		log.Error().Err(err).Str("installable", installable).Msg("could not build path to serve")
		os.Exit(sysexits.Unavailable)
	}

	server := &http.Server{Handler: siteHandler}

	// Spin up the server in a different goroutine.
	go func() {
		err := server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("server failed")
		}
	}()
	log.Info().Stringer("address", listener.Addr()).Msg("server started")

	// Watch for SIGINT and SIGTERM to shut down the server.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, unix.SIGINT)
	signal.Notify(interrupt, unix.SIGTERM)

	// Watch for SIGHUP, SIGUSR1 and SIGUSR2 to reload the server.
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, unix.SIGHUP)
	signal.Notify(reload, unix.SIGUSR1)
	signal.Notify(reload, unix.SIGUSR2)

	watchTargets := []watchTarget{}
	if enableTLS {
		watchTargets = append(
			watchTargets,
			watchTarget{path: filepath.Dir(*tlsCertificate), mask: fsevents.FileCreatedEvent | fsevents.CloseWrite},
			watchTarget{path: filepath.Dir(*tlsKey), mask: fsevents.FileCreatedEvent | fsevents.CloseWrite},
		)
	}
	watcher := watchAndStart(watchTargets)
	go watcher.Watch()

	for {
		select {
		case <-interrupt:
			log.Info().Msg("shutting down")
			if err := server.Shutdown(context.Background()); err != nil {
				log.Error().Err(err).Msg("server did not shut down cleanly")
			}
			return

		case <-reload:
			log.Info().Msg("reloading")
			if err := siteHandler.Realise(); err != nil {
				log.Error().Err(err).Str("installable", installable).Msg("could not build path to serve")
			}

		case event := <-watcher.Events:
			if enableTLS && (event.Path == *tlsCertificate || event.Path == *tlsKey) {
				log.Info().Msg("TLS certificate files updated; reloading")
				if err := loadTLSKeyPair(); err != nil {
					log.Error().Err(err).Msg("could not load TLS keypair")
				}
			}

		case err := <-watcher.Errors:
			log.Error().Err(err).Msg("filesystem watcher failed")
			os.Exit(sysexits.OSErr)
		}
	}
}

// watchTarget is a tuple representing an inotify watch.
type watchTarget struct {
	// The path being watched.
	path string
	// inotify mask watch.
	mask uint32
}

// watchAndStart sets up a fsevents.Watcher, adds watch descriptors to
// the given paths, and starts watching.
func watchAndStart(targets []watchTarget) *fsevents.Watcher {
	watcher, err := fsevents.NewWatcher()
	if err != nil {
		log.Error().Err(err).Msg("could not initialize filesystem watcher")
		os.Exit(sysexits.OSErr)
	}

	for _, target := range targets {
		descriptor, err := watcher.AddDescriptor(target.path, target.mask)
		if err != nil {
			log.Error().Err(err).Str("path", target.path).Msg("could not watch file")
			if errors.Is(err, fsevents.ErrDescAlreadyExists) {
				continue
			}
			os.Exit(sysexits.OSErr)
		}
		if err := descriptor.Start(); err != nil {
			log.Error().Err(err).Str("path", target.path).Msg("could not watch file")
			os.Exit(sysexits.OSErr)
		}
	}

	return watcher
}

// loadTLSKeyPair loads the X.509 certificate and key given in the
// command line into tlsKeyPair.
func loadTLSKeyPair() error {
	keypair, err := tls.LoadX509KeyPair(*tlsCertificate, *tlsKey)
	if err != nil {
		return err
	}
	tlsKeyPair.mu.Lock()
	defer tlsKeyPair.mu.Unlock()
	tlsKeyPair.certificate = &keypair
	log.Debug().Str("certificate_path", *tlsCertificate).Str("key_path", *tlsKey).Msg("loaded TLS keypair")
	return nil
}

// getCertificate returns the certificate in tlsKeyPair.
func getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	tlsKeyPair.mu.RLock()
	defer tlsKeyPair.mu.RUnlock()
	return tlsKeyPair.certificate, nil
}
