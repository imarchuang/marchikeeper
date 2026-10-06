package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/marchi/marchikeeper/internal/httpserver"
	"github.com/marchi/marchikeeper/internal/znodes"
)

func main() {
	listen := flag.String("listen", ":7181", "HTTP listen address")
	_ = flag.String("dataDir", "./data", "data directory (unused in slice 0)")
	_ = flag.String("id", "1", "node id")
	flag.Parse()

	srv := httpserver.New(znodes.New())
	log.Printf("marchikeeper listening on %s", *listen)
	if err := http.ListenAndServe(*listen, srv); err != nil {
		log.Fatal(err)
	}
}
