// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

	siteHandler := snowweb.SymlinkedStaticSiteServer{RootLink: *root}
	if err := siteHandler.Init(); err != nil {
		log.Fatalf("[error] initializing site siteHandler: %v\n", err)
	}
	server := &http.Server{Handler: &siteHandler}

	// Spin up the server in a different goroutine.
	go func() {
		err := server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[error] %v\n", err)
		}
	}()
	log.Printf("[info] listening on %s\n", listener.Addr())

	// Watch for SIGINT and SIGTERM.
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, syscall.SIGINT)
	signal.Notify(interrupted, syscall.SIGTERM)

	select {
	case <-interrupted:
		log.Printf("[debug] received signal\n")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Printf("[error] shutting down the server: %v\n", err)
		}
	}
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
