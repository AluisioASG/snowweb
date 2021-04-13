// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"flag"
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

var root = flag.String("root", ".", "Directory to serve files from")

func main() {
	flag.Parse()

	handler := StoreFileServer{Root: *root}
	if err := handler.RefreshRoot(); err != nil {
		log.Fatalf("[error] refreshing site root: %v\n", err)
	}
	server := &http.Server{
		Addr:    ":8080",
		Handler: &handler,
	}

	log.Fatal(server.ListenAndServe())
}

type StoreFileServer struct {
	// Path of the symbolic link pointing to the Nix store path to be
	// served.
	Root string
	// File system rooted at the actual Nix store path being served.
	resolvedRoot fs.FS
	// The ETag returned in responses and used during conditional
	// requests, derived from the resolved root path.
	etag string
}

// RefreshRoot re-resolves the root symlink and updates the computed
// ETag accordingly.
func (h *StoreFileServer) RefreshRoot() error {
	rootPath, err := filepath.EvalSymlinks(h.Root)
	if err != nil {
		return err
	}
	h.etag = "\"" + filepath.Base(rootPath) + "\""
	h.resolvedRoot = os.DirFS(rootPath)
	log.Printf("resolved %q to %q\n", h.Root, rootPath)
	return nil
}

// ServeHTTP responds to HTTP GET and HEAD requests with the
// corresponding file under the server root.  If the request
// is for a directory, the index.html file under that directory
// is served instead.
func (h *StoreFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		// TODO: return 405 (Allow: GET HEAD)
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
		// TODO: return 400
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
		// TODO: return 404
		return
	}
	if err != nil {
		log.Printf("[error] opening %q: %v\n", requestPath, err)
		// TODO: return 500
		return
	}
	// TODO: support Brotli

	// We're ready to serve the requested file.
	w.Header().Add("Cache-Control", "public, max-age=0, proxy-revalidate")
	w.Header().Add("Etag", h.etag)
	var noModTime time.Time
	http.ServeContent(w, r, requestPath, noModTime, f.(io.ReadSeeker))
	log.Printf("[info] served %q\n", r.URL.Path)
}
