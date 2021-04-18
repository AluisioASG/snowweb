// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"git.sr.ht/~aasg/snowweb"
	"git.sr.ht/~aasg/snowweb/internal/logwriter"
	"git.sr.ht/~aasg/snowweb/internal/sockaddr"
	"github.com/alecthomas/kong"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sean-/sysexits"
	"golang.org/x/sys/unix"
)

var cliArgs struct {
	Installable string `arg env:"SNOWWEB_INSTALLABLE" required help:"Package to serve."`

	ListenAddress string `name:"listen" env:"SNOWWEB_LISTEN" default:"tcp:[::1]:" help:"Address to listen at." placeholder:"ADDRESS"`
	Log           string `env:"SNOWWEB_LOG" default:"stderr" help:"Where to write log messages to." placeholder:"ADDRESS"`
	ClientCA      string `env:"SNOWWEB_CLIENT_CA_BUNDLE" help:"Path to TLS client CA bundle." placeholder:"PATH"`

	TLSCertificate string `env:"SNOWWEB_TLS_CERTIFICATE" help:"Path to TLS server certificate." placeholder:"PATH"`
	TLSKey         string `env:"SNOWWEB_TLS_KEY" help:"Path to TLS server certificate key." placeholder:"PATH"`
}

var tlsKeyPair struct {
	mu          sync.RWMutex
	certificate *tls.Certificate
}

func main() {
	kong.Parse(&cliArgs)

	// Set up zerolog to write to stderr by default, then switch to
	// whatever the user requests, for consistency in how we report
	// command-line errors.
	tempLogger := log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	logWriter, err := logwriter.Writer(cliArgs.Log)
	if err != nil {
		tempLogger.Error().Err(err).Str("address", cliArgs.Log).Msg("could not open log destination")
		os.Exit(sysexits.Unavailable)
	}
	log.Logger = log.Output(logWriter)

	// Have the "log" package's standard logger write to zerolog,
	// for consistency.
	stdlog.SetFlags(0)
	stdlog.SetOutput(log.Logger)

	listener, err := sockaddr.ListenerFromString(cliArgs.ListenAddress)
	if err != nil {
		log.Error().Err(err).Str("address", cliArgs.ListenAddress).Msg("could not create listening socket")
		os.Exit(sysexits.Unavailable)
	}

	enableTLS := cliArgs.TLSCertificate != ""
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

		if cliArgs.ClientCA != "" {
			pem, err := os.ReadFile(cliArgs.ClientCA)
			if err != nil {
				log.Error().Err(err).Str("path", cliArgs.ClientCA).Msg("could not read client CA bundle")
				os.Exit(sysexits.NoInput)
			}

			clientCAPool := x509.NewCertPool()
			if !clientCAPool.AppendCertsFromPEM(pem) {
				log.Error().Err(err).Str("path", cliArgs.ClientCA).Msg("could not parse certificates from client CA bundle")
				os.Exit(sysexits.DataErr)
			}

			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
			tlsConfig.ClientCAs = clientCAPool
			log.Debug().Str("ca_path", cliArgs.ClientCA).Msg("enabled client certificate verification")
		}

		listener = tls.NewListener(listener, &tlsConfig)
		log.Debug().Msg("TLS and HTTP/2 enabled")
	}

	// Create the handler and perform the initial build.
	siteHandler := snowweb.NewSnowWebServer(cliArgs.Installable)
	if err := siteHandler.Realise(); err != nil {
		log.Error().Err(err).Str("installable", cliArgs.Installable).Msg("could not build path to serve")
		os.Exit(sysexits.Unavailable)
	}

	server := &http.Server{
		Handler: siteHandler,
		// Timeout requests to mitigate slowloris attacks, but do not
		// timeout response writes to avoid failing large downloads on
		// slow connections.  Remote-triggered rebuilds would also run
		// foul of a write timeout, because it covers the entirety of
		// the handler's runtime.
		IdleTimeout:       5 * time.Minute,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
	}

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

	// Watch for SIGHUP, SIGUSR1 and SIGUSR2 to reload (parts of) the
	// server.
	reloadRoot := make(chan os.Signal, 1)
	reloadTLS := make(chan os.Signal, 1)
	signal.Notify(reloadRoot, unix.SIGHUP)
	signal.Notify(reloadRoot, unix.SIGUSR1)
	signal.Notify(reloadTLS, unix.SIGHUP)
	signal.Notify(reloadTLS, unix.SIGUSR2)

	for {
		select {
		case <-interrupt:
			log.Info().Msg("shutting down")
			if err := server.Shutdown(context.Background()); err != nil {
				log.Error().Err(err).Msg("server did not shut down cleanly")
			}
			return

		case <-reloadRoot:
			log.Info().Msg("rebuilding root path")
			if err := siteHandler.Realise(); err != nil {
				log.Error().Err(err).Str("installable", cliArgs.Installable).Msg("could not build path to serve")
			}

		case <-reloadTLS:
			log.Info().Msg("reloading TLS keypair")
			if err := loadTLSKeyPair(); err != nil {
				log.Error().Err(err).Msg("could not load TLS keypair")
			}
		}
	}
}

// loadTLSKeyPair loads the X.509 certificate and key given in the
// command line into tlsKeyPair.
func loadTLSKeyPair() error {
	keypair, err := tls.LoadX509KeyPair(cliArgs.TLSCertificate, cliArgs.TLSKey)
	if err != nil {
		return err
	}
	tlsKeyPair.mu.Lock()
	defer tlsKeyPair.mu.Unlock()
	tlsKeyPair.certificate = &keypair
	log.Debug().Str("certificate_path", cliArgs.TLSCertificate).Str("key_path", cliArgs.TLSKey).Msg("loaded TLS keypair")
	return nil
}

// getCertificate returns the certificate in tlsKeyPair.
func getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	tlsKeyPair.mu.RLock()
	defer tlsKeyPair.mu.RUnlock()
	return tlsKeyPair.certificate, nil
}
