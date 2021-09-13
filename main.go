package main

import (
	"math/rand"
	"net/http"
	"time"
	"turtleC&C/websocket"
)

func main() {
	rand.Seed(time.Now().Unix())
	hub := websocket.NewHub()
	http.HandleFunc("/admin/ws", func(w http.ResponseWriter, r *http.Request) {
		websocket.ServeWs(hub, w, r)
	})
	http.ListenAndServe("0.0.0.0:50806", nil)
}

