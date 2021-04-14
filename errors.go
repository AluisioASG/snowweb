// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package snowweb

import (
	"net/http"
)

// Errors passed by SnowWeb to error handlers.
const (
	_                      = iota
	ErrorUnsupportedMethod // Request is not GET or HEAD
	ErrorInvalidPath       // Invalid request path (e.g. contains "..")
	ErrorNotFound          // Requested file does not exist
	ErrorIO                // I/O error opening the requested file
)

// HandleError is the default SnowWeb error handler.
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
