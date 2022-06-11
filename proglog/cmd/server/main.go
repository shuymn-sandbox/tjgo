package main

import (
	"log"

	"github.com/shuymn-sandbox/tjgo/proglog/internal/server"
)

func main() {
	srv := server.NewHTTPServer("127.0.0.1:8888")
	log.Fatal(srv.ListenAndServe())
}
