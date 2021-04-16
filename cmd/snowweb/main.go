// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"git.sr.ht/~aasg/snowweb"
	"git.sr.ht/~aasg/snowweb/internal/listeners"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sean-/sysexits"
	"github.com/tywkeene/go-fsevents"
	"golang.org/x/sys/unix"
)

var listenAddress = flag.String("listen", "tcp:[::1]:", "TCP or Unix socket address to listen at")
var tlsCertificate = flag.String("certificate", "", "Path to TLS certificate")
var tlsKey = flag.String("key", "", "Path to TLS key")

var tlsKeyPair struct {
	mu          sync.RWMutex
	certificate *tls.Certificate
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	flag.Parse()

	if flag.NArg() != 1 {
		log.Error().Msg("no installable given in the command line")
		os.Exit(sysexits.Usage)
	}
	installable := flag.Arg(0)

	listener, err := listeners.FromString(*listenAddress)
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
		listener = tls.NewListener(listener, &tls.Config{
			GetCertificate:           getCertificate,
			MinVersion:               tls.VersionTLS12,
			NextProtos:               []string{"h2", "http/1.1"},
			PreferServerCipherSuites: true,
			SessionTicketsDisabled:   true,
		})
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
	return nil
}

// getCertificate returns the certificate in tlsKeyPair.
func getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	tlsKeyPair.mu.RLock()
	defer tlsKeyPair.mu.RUnlock()
	return tlsKeyPair.certificate, nil
}
