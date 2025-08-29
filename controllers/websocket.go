package controllers

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"temp0ral-chat/helpers"
	"temp0ral-chat/models"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn  *websocket.Conn
	mutex sync.Mutex
}

type Hub struct {
	clients   map[*Client]bool
	broadcast chan string
	mutex     sync.Mutex
}

var GlobalHub = Hub{
	clients:   make(map[*Client]bool),
	broadcast: make(chan string),
}

func WebSocketHandler(c *gin.Context) {
	session, exists := c.Get("session")
	if !exists {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}

	userSession := session.(models.Session)

	helpers.UpdateUserActivity(userSession.UserID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{conn: conn}
	GlobalHub.mutex.Lock()
	GlobalHub.clients[client] = true
	GlobalHub.mutex.Unlock()

	BroadcastUserList()

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			client.mutex.Lock()
			client.conn.Close()
			client.mutex.Unlock()
			GlobalHub.mutex.Lock()
			delete(GlobalHub.clients, client)
			GlobalHub.mutex.Unlock()
			BroadcastUserList()
			return
		}

		_ = messageBytes

		if _, valid := GetSession(userSession.ID); !valid {
			errorHTML := `<div hx-swap-oob="innerHTML:#error-container">
				<div class="error-message">
					Your session has expired. Please <a href="/" class="error-link">refresh the page</a> to continue.
				</div>
			</div>`

			client.mutex.Lock()
			err := client.conn.WriteMessage(websocket.TextMessage, []byte(errorHTML))
			client.mutex.Unlock()
			if err != nil {
				log.Println("Error sending session expired message:", err)
			}

			time.Sleep(100 * time.Millisecond)
			client.mutex.Lock()
			client.conn.Close()
			client.mutex.Unlock()
			return
		}
	}
}

func BroadcastUserList() {
	activeSessions := helpers.GetActiveSessions()

	userListHTML := `<div hx-swap-oob="innerHTML:#user-list">`
	for _, session := range activeSessions {
		shortID := session.UserID[:8]
		status := helpers.GetUserStatus(session.UserID)
		statusClass := "user-status-" + status

		userListHTML += `<div class="user-item" title="Session ID: ` + session.UserID + `">
			<span class="user-status ` + statusClass + `"></span>
			<span class="user-id">` + shortID + `</span>
		</div>`
	}
	userListHTML += `</div>`

	countHTML := `<div hx-swap-oob="innerHTML:.user-count">` +
		fmt.Sprintf("%d online", len(activeSessions)) + `</div>`

	fullUpdate := userListHTML + countHTML

	GlobalHub.mutex.Lock()
	for client := range GlobalHub.clients {
		client.mutex.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, []byte(fullUpdate))
		client.mutex.Unlock()
		if err != nil {
			client.conn.Close()
			GlobalHub.mutex.Lock()
			delete(GlobalHub.clients, client)
			GlobalHub.mutex.Unlock()
		}
	}
	GlobalHub.mutex.Unlock()
}

func (h *Hub) RunSocket() {
	for msg := range h.broadcast {
		h.mutex.Lock()
		for client := range h.clients {
			client.mutex.Lock()
			err := client.conn.WriteMessage(websocket.TextMessage, []byte(msg))
			client.mutex.Unlock()
			if err != nil {
				client.conn.Close()
				h.mutex.Lock()
				delete(h.clients, client)
				h.mutex.Unlock()
			}
		}
		h.mutex.Unlock()
	}
}
