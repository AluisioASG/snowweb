// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"

	"git.sr.ht/~aasg/snowweb"
)

var listenAddress = flag.String("listen", "[::1]:0", "TCP or Unix socket address to listen at")
var root = flag.String("root", ".", "Directory to serve files from")

func main() {
	flag.Parse()

	listener, err := net.Listen(parseListenAddress(*listenAddress))
	if err != nil {
		log.Fatalf("[error] binding to %s: %v\n", *listenAddress, err)
	}

	handler := snowweb.SymlinkedStaticSiteServer{RootLink: *root}
	if err := handler.Init(); err != nil {
		log.Fatalf("[error] initializing site handler: %v\n", err)
	}
	server := &http.Server{Handler: &handler}

	log.Printf("[info] listening on %s\n", listener.Addr())
	log.Fatal(server.Serve(listener))
}

// parseListenAddress detects whether a string represents an IP
// address/port combinator or an Unix socket and returns a pair
// of strings that can be passed as parameters to net.Listen.
func parseListenAddress(s string) (string, string) {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return "unix", s
	}
	return "tcp", s
}
