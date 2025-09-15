package main

import (
	"log"
	"net/http"

	"github.com/xh1126xx/gochatx/internal/gateway"
)

func main() {
	http.HandleFunc("/ws", gateway.HandleWS)
	http.Handle("/", http.FileServer(http.Dir("./web")))
	log.Println("Gateway listening on :8080")
	http.ListenAndServe(":8080", nil)
}
