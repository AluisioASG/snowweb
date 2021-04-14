// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package snowweb

import (
	"fmt"
	"log"
	"net/http"

	"git.sr.ht/~aasg/snowweb/internal/nix"
)

// A SnowWebServer is an http.Handler that serves static files
// from a Nix store path.
type SnowWebServer struct {
	// Function called to produce an error response in case an error
	// happens while handling a request.
	Error ErrorHandler
	// Inner Nix store path file server.
	fileServer *NixStorePathServer
	// The Nix installable whose `out` output path is served.
	installable string
	// HTTP request matcher used to split request handling between
	// regular files and the SnowWeb API.
	mux *http.ServeMux
}

// NewSnowWebServer constructs a new SnowWebServer.
//
// After getting a SnowWebServer, Realise must be called to perform the
// initial build and set the served path before a request comes through.
func NewSnowWebServer(installable string) (*SnowWebServer, error) {
	h := SnowWebServer{
		Error:       HandleError,
		installable: installable,
		mux:         http.NewServeMux(),
	}

	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.fileServer.ServeHTTP(w, r)
	})
	// Block the .snowweb directory, except for the API endpoints
	// which are handled later on.
	h.mux.HandleFunc("/.snowweb/", func(w http.ResponseWriter, r *http.Request) {
		h.Error(ErrorNotFound, w, r)
	})

	h.mux.HandleFunc("/.snowweb/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			h.Error(ErrorUnsupportedMethod, w, r)
			return
		}
		fmt.Fprintf(w, "status: ok\npath: %v\n", h.fileServer.StorePath())
	})

	return &h, nil
}

func (h *SnowWebServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// Realise builds the Nix installable and updates the server to serve
// the resulting store path.
func (h *SnowWebServer) Realise() error {
	storePath, err := nix.Build(h.installable)
	if err != nil {
		return err
	}
	log.Printf("[debug] built %v to %v\n", h.installable, storePath)

	fileServer, err := NewNixStorePathServer(storePath)
	if err != nil {
		return err
	}

	fileServer.Error = func(code int, w http.ResponseWriter, r *http.Request) {
		h.Error(code, w, r)
	}

	h.fileServer = fileServer
	log.Printf("[debug] now serving %v\n", h.fileServer.StorePath())
	return nil
}
