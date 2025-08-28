package models

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	Conn  *websocket.Conn
	mutex sync.Mutex
}

type Hub struct {
	Clients   map[*Client]bool
	Broadcast chan string
	Mutex     sync.Mutex
}

var GlobalHub = Hub{
	Clients:   make(map[*Client]bool),
	Broadcast: make(chan string),
}
