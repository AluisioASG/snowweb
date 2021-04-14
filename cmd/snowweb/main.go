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
	"path/filepath"
	"strings"

	"git.sr.ht/~aasg/snowweb"
	"github.com/tywkeene/go-fsevents"
	"golang.org/x/sys/unix"
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
	signal.Notify(interrupted, unix.SIGINT)
	signal.Notify(interrupted, unix.SIGTERM)

	watchTargets := []watchTarget{
		// Watch for changes in the directory of the root symlink.
		{path: filepath.Dir(siteHandler.RootLink), mask: unix.IN_DONT_FOLLOW | unix.IN_CREATE | unix.IN_MOVED_TO},
	}
	watcher := watchAndStart(watchTargets)
	go watcher.Watch()

	for {
		select {
		case <-interrupted:
			log.Printf("[debug] received signal\n")
			if err := server.Shutdown(context.Background()); err != nil {
				log.Printf("[error] shutting down the server: %v\n", err)
			}
			return

		case event := <-watcher.Events:
			if event.Path == siteHandler.RootLink {
				log.Printf("[debug] root link updated\n")
				if err := siteHandler.RefreshRoot(); err != nil {
					log.Printf("[error] reloading root: %v\n", err)
				}
			}

		case err := <-watcher.Errors:
			log.Fatalf("[error] watching root path: %v\n", err)
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
		log.Fatalf("[error] setting up file monitoring: %v\n", err)
	}

	for _, target := range targets {
		descriptor, err := watcher.AddDescriptor(target.path, target.mask)
		if err != nil {
			log.Fatalf("[error] monitoring %q: %v\n", target.path, err)
		}
		if err := descriptor.Start(); err != nil {
			log.Fatalf("[error] monitoring %q: %v\n", target.path, err)
		}
	}

	return watcher
}
