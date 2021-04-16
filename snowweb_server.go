// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package snowweb

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"

	"git.sr.ht/~aasg/snowweb/internal/nix"
	"github.com/kevinpollet/nego"
	"github.com/rs/zerolog/log"
)

// A SnowWebServer is an http.Handler that serves static files
// from a Nix store path.
type SnowWebServer struct {
	// Function called to check if a request for an API action may be
	// executed.  If not set, it defaults to snowweb.authorizeRequest.
	AuthorizeRequest func(r *http.Request) bool
	// Function called to produce an error response in case an error
	// happens while handling a request.  If not set, it defaults to
	// snowweb.HandleError.
	Error ErrorHandler
	// Site-specific HTTP headers send with every response.
	extraHeaders textproto.MIMEHeader
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
func NewSnowWebServer(installable string) *SnowWebServer {
	h := SnowWebServer{
		AuthorizeRequest: authorizeRequest,
		Error:            HandleError,
		installable:      installable,
		mux:              http.NewServeMux(),
	}

	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.fileServer.ServeHTTP(w, r)
	})
	// Block the .snowweb directory, except for the API endpoints
	// which are handled later on.
	h.mux.HandleFunc("/.snowweb/", func(w http.ResponseWriter, r *http.Request) {
		h.Error(ErrorNotFound, w, r)
	})

	h.mux.HandleFunc("/.snowweb/reload", h.serveReload)
	h.mux.HandleFunc("/.snowweb/status", h.serveStatus)

	return &h
}

func (h *SnowWebServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Write out the extra headers before passing the request to our mux
	// for the actual response.
	responseHeaders := w.Header()
	for name, values := range h.extraHeaders {
		for _, value := range values {
			responseHeaders.Add(name, value)
		}
	}

	h.mux.ServeHTTP(w, r)
}

// Realise builds the Nix installable and updates the server to serve
// the resulting store path.
func (h *SnowWebServer) Realise() error {
	// Build the derivation we'll be serving.
	storePath, err := nix.Build(h.installable)
	if err != nil {
		return fmt.Errorf("snowweb: building %v: %w", h.installable, err)
	}
	log.Debug().Str("installable", h.installable).Str("path", storePath).Msg("built Nix package")

	// Set up the new static file server.
	fileServer, err := NewNixStorePathServer(storePath)
	if err != nil {
		return fmt.Errorf("snowweb: creating NixStorePathServer for %q: %w", storePath, err)
	}
	fileServer.Error = func(code int, w http.ResponseWriter, r *http.Request) {
		h.Error(code, w, r)
	}

	// Try reading site-specific headers, if there are any.
	headersPath := filepath.Join(storePath, ".snowweb", "headers")
	headers, err := readMIMEHeaders(headersPath)
	if err != nil {
		return fmt.Errorf("snowweb: reading site-specific headers from %q: %w", headersPath, err)
	}
	log.Debug().Str("path", headersPath).Msg("read site-specific headers")

	// Switch to the new derivation.
	h.fileServer = fileServer
	h.extraHeaders = headers
	log.Info().Str("path", h.fileServer.StorePath()).Msg("changed site root")
	return nil
}

// serveStatus responds to a request to the /.snowweb/status endpoint.
func (h *SnowWebServer) serveStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept")

	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Add("Allow", "GET, HEAD")
		w.Header().Add("Content-Length", "0")
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}

	response := struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}{OK: true, Path: h.fileServer.StorePath()}

	switch nego.NegotiateContentType(r, "text/plain", "application/json") {
	case "application/json":
		data, err := json.Marshal(response)
		if err != nil {
			log.Error().Err(err).Msg("could not marshal JSON response to status endpoint")
			// Fall through to the default response format.
			break
		}

		w.Header().Add("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Default response format.
	fmt.Fprintf(w, "ok\nserving %v\n", response.Path)
}

// serveReload responds to a request to the /.snowweb/reload endpoint.
func (h *SnowWebServer) serveReload(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Accept")

	if r.Method != "POST" {
		w.Header().Add("Allow", "POST")
		w.Header().Add("Content-Length", "0")
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}

	log.Info().Str("address", r.RemoteAddr).Msg("processing remote rebuild request")
	if !h.AuthorizeRequest(r) {
		w.Header().Add("Content-Length", "0")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	err := h.Realise()
	if err != nil {
		log.Error().Err(err).Msg("could not rebuild website")
	}

	response := struct {
		OK    bool   `json:"ok"`
		Path  string `json:"path,omitempty"`
		Error error  `json:"error,omitempty"`
	}{OK: err == nil, Error: err}
	if err == nil {
		response.Path = h.fileServer.StorePath()
	}

	switch nego.NegotiateContentType(r, "text/plain", "application/json") {
	case "application/json":
		data, err := json.Marshal(response)
		if err != nil {
			log.Error().Err(err).Msg("could not marshal JSON response to status endpoint")
			// Fall through to the default response format.
			break
		}

		// TODO: should we not send a 200 when the rebuild fails?
		w.Header().Add("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Default response format.
	if response.OK {
		fmt.Fprintf(w, "ok\nserving %v\n", response.Path)
	} else {
		fmt.Fprintf(w, "error\n%v\n", response.Error)
	}
}

// readMIMEHeaders reads a MIME-style header from a file.
//
// If the file does not exist, an empty header is returned instead of
// an error.
func readMIMEHeaders(filename string) (textproto.MIMEHeader, error) {
	f, err := os.Open(filename)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return make(textproto.MIMEHeader), nil
	case err != nil:
		return nil, err
	}
	defer f.Close()

	tpReader := textproto.NewReader(bufio.NewReader(f))
	return tpReader.ReadMIMEHeader()
}

// authorizeRequest authorizes API commands by verifying that the
// connection was made over TLS and that a client certificate was
// presented and verified.
func authorizeRequest(r *http.Request) bool {
	switch {
	default:
		crt := r.TLS.VerifiedChains[0][0]
		log.Info().Str("url_path", r.URL.Path).Stringer("serial", crt.SerialNumber).Msg("authenticated client for remote command")
		return true
	case r.TLS == nil:
		fallthrough
	case len(r.TLS.VerifiedChains) == 0:
		fallthrough
	case len(r.TLS.VerifiedChains[0]) == 0:
		return false
	}
}
