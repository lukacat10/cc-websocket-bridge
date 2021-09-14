package main

import (
	"log"
	"math/rand"
	"net/http"
	"time"
	"turtleCandC/websocket"
)

func main() {
	log.Println("Server is now running on port 50806")
	rand.Seed(time.Now().Unix())
	hub := websocket.NewHub()
	go hub.Run()
	http.HandleFunc("/admin/ws", func(w http.ResponseWriter, r *http.Request) {
		websocket.ServeWs(hub, w, r)
	})
	http.ListenAndServe("0.0.0.0:50806", nil)
}

