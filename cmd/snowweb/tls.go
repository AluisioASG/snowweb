// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"path/filepath"

	"git.sr.ht/~aasg/snowweb/internal/certpool"
	"git.sr.ht/~aasg/snowweb/internal/logwriter"
	"github.com/alecthomas/kong"
	"github.com/caddyserver/certmagic"
	"github.com/emersion/go-appdir"
	"github.com/rs/zerolog/log"
)

// Constants defining the source of TLS certificates.
const (
	certSourceNone = iota // No source configured.
	certSourceFile        // Certificates loaded from the filesystem.
	certSourceAcme        // Certificates provisioned through ACME.
)

var TLSVars = kong.Vars{
	"tlsDefaultCA":      certmagic.LetsEncryptProductionCA,
	"tlsDefaultStorage": filepath.Join(appdir.New("snowweb").UserData(), "certstorage"),
}

// TLSArgs holds the TLS command-line configuration.
type TLSArgs struct {
	// Source of TLS certificates.
	source int
	// The CertMagic instance used to manage TLS certificates.
	magic *certmagic.Config

	// These fields are used when source = certSourceFile.
	Certificate string `env:"SNOWWEB_TLS_CERTIFICATE" help:"Path to TLS server certificate." placeholder:"PATH" group:"File-based TLS"`
	Key         string `env:"SNOWWEB_TLS_KEY" help:"Path to TLS server certificate key." placeholder:"PATH" group:"File-based TLS"`

	// These fields are used when source = certSourceAcme.
	ACME ACMEArgs `embed prefix:"acme-"`
}

// ACMEArgs holds the ACME command-line configuration.
type ACMEArgs struct {
	Domains []string `env:"SNOWWEB_TLS_ACME_DOMAINS" help:"Domains to obtain TLS certificates for." placeholder:"DOMAIN" group:"ACME-based TLS"`
	CA      string   `name:"ca" env:"SNOWWEB_TLS_ACME_CA" help:"URL of the ACME directory." default:"${tlsDefaultCA}" group:"ACME-based TLS"`
	CARoots string   `name:"ca-roots" env:"SNOWWEB_TLS_ACME_CA_ROOTS" help:"Path to ACME CA certificate bundle." group:"ACME-based TLS" placeholder:"PATH"`
	Email   string   `env:"SNOWWEB_TLS_ACME_EMAIL" help:"Email address to register an ACME account with." placeholder:"EMAIL" group:"ACME-based TLS"`
	Storage string   `env:"SNOWWEB_TLS_ACME_STORAGE" help:"Where to store provisioned certificates." default:"${tlsDefaultStorage}" placeholder:"PATH" group:"ACME-based TLS"`
}

// Validate ensures that the TLS-related command-line flags are
// internally consistent.
func (args *TLSArgs) Validate() error {
	if (args.Certificate != "") != (args.Key != "") {
		return errors.New("--certificate and --key must be either both given, or both not given")
	}
	fileSourceEnabled := args.Certificate != ""

	acmeSourceEnabled := len(args.ACME.Domains) > 0

	switch {
	case fileSourceEnabled && acmeSourceEnabled:
		return errors.New("ACME and local certificate cannot be both enabled")
	case fileSourceEnabled:
		args.source = certSourceFile
	case acmeSourceEnabled:
		args.source = certSourceAcme
	}
	return nil
}

// Init initializes the CertMagic instance within TLSArgs.
func (args *TLSArgs) Init() error {
	zapLogger := (&logwriter.ZapToZerologAdapter{Logger: &log.Logger}).ZapLogger()

	certmagic.Default.Logger = zapLogger
	certmagic.Default.Storage = &certmagic.FileStorage{Path: args.ACME.Storage}

	certmagic.DefaultACME.Agreed = true
	certmagic.DefaultACME.CA = args.ACME.CA
	certmagic.DefaultACME.Email = args.ACME.Email
	certmagic.DefaultACME.DisableHTTPChallenge = true
	certmagic.DefaultACME.Logger = zapLogger

	if args.ACME.CARoots != "" {
		caPool, err := certpool.LoadX509CertPool(args.ACME.CARoots)
		if err != nil {
			return fmt.Errorf("loading ACME CA roots: %w", err)
		}
		certmagic.DefaultACME.TrustedRoots = caPool
	}

	args.magic = certmagic.NewDefault()

	switch args.source {
	case certSourceFile:
		log.Debug().Msg("initialized file-based certificate management")
	case certSourceAcme:
		log.Debug().Msg("initialized ACME-based certificate management")
	case certSourceNone:
		log.Debug().Msg("disabled certificate management")
	}

	return nil
}

// Config constructs a tls.Config that loads certificates according
// to the settings specified by the user.
func (args *TLSArgs) Config() *tls.Config {
	tlsConfig := args.magic.TLSConfig()
	tlsConfig.NextProtos = append([]string{"h2", "http/1.1"}, tlsConfig.NextProtos...)
	tlsConfig.SessionTicketsDisabled = true
	return tlsConfig
}

// ReloadCerts forces reloading of the TLS certificate and key.
//
// If the TLS keypair is loaded from the file system, it is re-read
// from the source files.  If the certificate is provisioned through
// ACME, it is renewed if it's close to expiring.
func (args *TLSArgs) ReloadCerts() error {
	switch args.source {
	case certSourceFile:
		return args.magic.CacheUnmanagedCertificatePEMFile(args.Certificate, args.Key, nil)
	case certSourceAcme:
		return args.magic.ManageSync(args.ACME.Domains)
	default:
		return nil
	}
}

// Enabled returns true if TLS support was enabled in the command line.
func (args *TLSArgs) Enabled() bool {
	return args.source != certSourceNone
}
