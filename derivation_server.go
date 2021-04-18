// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package snowweb

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"git.sr.ht/~aasg/snowweb/internal/nix"
	"github.com/kevinpollet/nego"
	"github.com/rs/zerolog/log"
)

// A NixStorePathServer is an http.Handler that serves static files
// from a Nix store path.
type NixStorePathServer struct {
	// Function called to respond to a request in case an error happens
	// while handling the request.
	Error ErrorHandler
	// The ETag returned in responses and used during conditional
	// requests, derived from the resolved root path.
	etag string
	// File system rooted at the actual directory being served.
	resolvedRoot fs.FS
	// Nix store path being served.
	storePath string
}

// NewNixStorePathServer constructs a new NixStorePathServer.
func NewNixStorePathServer(storePath string) (*NixStorePathServer, error) {
	narHash, err := nix.NarHash(storePath)
	if err != nil {
		return nil, err
	}

	h := NixStorePathServer{
		etag:         "\"" + narHash + "\"",
		Error:        HandleError,
		resolvedRoot: os.DirFS(storePath),
		storePath:    storePath,
	}
	return &h, nil
}

// StorePath returns the Nix store path being served.
func (h *NixStorePathServer) StorePath() string {
	return h.storePath
}

// ServeHTTP responds to HTTP GET and HEAD requests with the
// corresponding file under the server root.  If the request
// is for a directory, the index.html file under that directory
// is served instead.
func (h *NixStorePathServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	log.Debug().Str("url_path", r.URL.Path).Str("file_path", requestPath).Msg("rewritten request path")
	// If the path is not valid, reject the request.
	if !fs.ValidPath(requestPath) {
		log.Error().Str("url_path", r.URL.Path).Msg("invalid request path")
		h.Error(ErrorInvalidPath, w, r)
		return
	}

	// Open the requested file.  If it's a directory, try reading
	// index.html within it.
	f, requestPath, err := h.openFile(requestPath, true)
	defer closeOrLog(requestPath, f)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		h.Error(ErrorNotFound, w, r)
		return
	case err != nil:
		log.Error().Err(err).Str("file_path", requestPath).Msg("could not open file")
		h.Error(ErrorIO, w, r)
		return
	}

	// If the client supports Brotli and a precompressed file is
	// available, send it.
	w.Header().Add("Vary", "Accept-Encoding")
	if brotliSupported(r) {
		fbr, _, err := h.openFile(requestPath+".br", false)
		defer closeOrLog(requestPath+".br", fbr)
		switch {
		default:
			f = fbr
			w.Header().Add("Content-Encoding", "br")
			log.Debug().Str("file_path", requestPath+".br").Msg("sending precompressed file")
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			log.Error().Err(err).Str("file_path", requestPath+".br").Msg("could not open precompressed file")
		}
	}

	// We're ready to serve the requested file.
	var zeroTime time.Time
	w.Header().Add("Cache-Control", "public, max-age=0, proxy-revalidate")
	w.Header().Add("Etag", h.etag)
	http.ServeContent(w, r, requestPath, zeroTime, f)
}

// openFile opens a file under the site root and returns an
// I/O value suitable for passing to http.ServeContent.
//
// If redirectDirectory is true, a request to open a directory
// will be rewritten to open index.html in that directory, if it
// exists.  The second return value can be used to determine the
// actual file opened.
func (h *NixStorePathServer) openFile(filename string, redirectDirectory bool) (io.ReadSeekCloser, string, error) {
	f, err := h.resolvedRoot.Open(filename)
	if err != nil {
		return nil, filename, err
	}

	// If the ‟file” is actually a directory and we're told to redirect
	// those, do so.
	stat, err := f.Stat()
	if err != nil {
		return nil, filename, err
	}
	if stat.IsDir() && redirectDirectory {
		closeOrLog(filename, f)
		return h.openFile(filename+"/index.html", redirectDirectory)
	}

	return f.(io.ReadSeekCloser), filename, nil
}

// brotliSupported checks whether Brotli compression is supported by
// the user agent (as announced in the Accept-Encoding header).
func brotliSupported(r *http.Request) bool {
	selectedEncoding := nego.NegotiateContentEncoding(r, nego.EncodingIdentity, "br")
	return selectedEncoding == "br"
}

// closeOrLog calls v.Close, and logs any error that gets returned.
//
// If v is nil, nothing happens.
func closeOrLog(name string, v io.Closer) {
	if v == nil {
		return
	}
	if err := v.Close(); err != nil {
		log.Error().Err(err).Str("file_path", name).Msg("could not close file")
	}
}
