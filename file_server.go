// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package snowweb

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Errors passed for handling by SymlinkedStaticSiteServer.Error.
const (
	_                      = iota
	ErrorUnsupportedMethod // Request is not GET or HEAD
	ErrorInvalidPath       // Invalid request path (e.g. contains "..")
	ErrorNotFound          // Requested file does not exist
	ErrorIO                // I/O error opening the requested file
)

// zeroTime is passed to http.ServeContent as the modification time
// to disable time-based conditional checks.
var zeroTime time.Time

// A SymlinkedStaticSiteServer is an http.Handler that serves files
// from a directory.
//
// The SymlinkedStaticSiteServer differs from an http.FileServer in
// that the path to the directory being served is expected to be a
// symbolic link pointing to a particular immutable deployment of a
// website, a fact that is used when handling the various HTTP caching
// headers.
type SymlinkedStaticSiteServer struct {
	// Handler function called to respond to a request in case
	// an error happens while handling the request.  If nil,
	// snowweb.HandleError is called.
	Error func(errorCode int, w http.ResponseWriter, r *http.Request)
	// Path of the symbolic link pointing to the directory to be served.
	RootLink string
	// The ETag returned in responses and used during conditional
	// requests, derived from the resolved root path.
	etag string
	// File system rooted at the actual directory being served.
	resolvedRoot fs.FS
}

// Init initializes internal fields of a SymlinkedStaticSiteServer and
// performs the first resolution of the root symlink.  This method must
// be called before the SymlinkedStaticSiteServer is used.
func (h *SymlinkedStaticSiteServer) Init() error {
	if h.Error == nil {
		h.Error = HandleError
	}
	if err := h.RefreshRoot(); err != nil {
		return err
	}
	return nil
}

// RefreshRoot re-resolves the root symlink and updates the computed
// ETag accordingly.
func (h *SymlinkedStaticSiteServer) RefreshRoot() error {
	rootPath, err := filepath.EvalSymlinks(h.RootLink)
	if err != nil {
		return fmt.Errorf("snowweb: resolving symlink %q: %w", h.RootLink, err)
	}
	h.etag = "\"" + filepath.Base(rootPath) + "\""
	h.resolvedRoot = os.DirFS(rootPath)
	log.Printf("resolved %q to %q\n", h.RootLink, rootPath)
	return nil
}

// ServeHTTP responds to HTTP GET and HEAD requests with the
// corresponding file under the server root.  If the request
// is for a directory, the index.html file under that directory
// is served instead.
func (h *SymlinkedStaticSiteServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		h.Error(ErrorUnsupportedMethod, w, r)
		return
	}

	// Construct the path to the file we're supposed to serve.
	//
	// First, do some preprocessing to ensure that expected paths are
	// considered valid.
	requestPath := strings.TrimLeft(r.URL.Path, "/")
	if requestPath == "" {
		requestPath = "index.html"
	} else if strings.HasSuffix(requestPath, "/") {
		requestPath += "/index.html"
	}
	log.Printf("[debug] rewritten request path from %q to %q\n", r.URL.Path, requestPath)
	// If the path is not valid, reject the request.
	if !fs.ValidPath(requestPath) {
		log.Printf("[error] invalid request path: %q\n", r.URL.Path)
		h.Error(ErrorInvalidPath, w, r)
		return
	}

	// Open the requested file.  If it's a directory, try reading
	// index.html within it.
	f, err := h.resolvedRoot.Open(requestPath)
	if errors.Is(err, syscall.EISDIR) {
		requestPath += "/index.html"
		f, err = h.resolvedRoot.Open(requestPath)
	}
	if errors.Is(err, fs.ErrNotExist) {
		h.Error(ErrorNotFound, w, r)
		return
	}
	if err != nil {
		log.Printf("[error] opening %q: %v\n", requestPath, err)
		h.Error(ErrorIO, w, r)
		return
	}
	// TODO: support Brotli

	// We're ready to serve the requested file.
	w.Header().Add("Cache-Control", "public, max-age=0, proxy-revalidate")
	w.Header().Add("Etag", h.etag)
	http.ServeContent(w, r, requestPath, zeroTime, f.(io.ReadSeeker))
	log.Printf("[info] served %q\n", r.URL.Path)
}

// HandleError is the default error handler for SymlinkedStaticSiteServer.
//
// When HandleError is called, a response is written with the
// appropriate HTTP status code and no body.
func HandleError(errorCode int, w http.ResponseWriter, r *http.Request) {
	switch errorCode {
	case ErrorUnsupportedMethod:
		w.Header().Add("Allow", "GET, HEAD")
		w.Header().Add("Content-Length", "0")
		w.WriteHeader(http.StatusMethodNotAllowed)
	case ErrorInvalidPath:
		w.Header().Add("Content-Length", "0")
		w.WriteHeader(http.StatusBadRequest)
	case ErrorNotFound:
		w.Header().Add("Content-Length", "0")
		w.WriteHeader(http.StatusNotFound)
	case ErrorIO:
		w.Header().Add("Content-Length", "0")
		w.WriteHeader(http.StatusInternalServerError)
	}
}
