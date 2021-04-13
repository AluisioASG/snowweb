// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"log"
	"net/http"

	"git.sr.ht/~aasg/snowweb"
)

var root = flag.String("root", ".", "Directory to serve files from")

func main() {
	flag.Parse()

	handler := snowweb.SymlinkedStaticSiteServer{RootLink: *root}
	if err := handler.Init(); err != nil {
		log.Fatalf("[error] initializing site handler: %v\n", err)
	}
	server := &http.Server{
		Addr:    ":8080",
		Handler: &handler,
	}

	log.Fatal(server.ListenAndServe())
}
