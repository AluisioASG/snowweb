// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package certpool

import (
	"crypto/x509"
	"fmt"
	"os"
)

// LoadX509CertPool reads and parses a sequence of PEM-encoded
// certificates from a file.
//
// No errors in individual certificates are reported; the function only
// fails if no certificate can be parsed.
func LoadX509CertPool(bundleFile string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(bundleFile)
	if err != nil {
		return nil, fmt.Errorf("certpool: loading certificates from %v: %w", bundleFile, err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("certpool: parsing certificates from %v: no certificates could be parsed", bundleFile)
	}

	return certPool, nil
}
