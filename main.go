package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"temp0ral-chat/models"
	"temp0ral-chat/templates"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
)

const (
	dbUser     = "changeme"
	dbPassword = "changeme"
	dbName     = "changeme"
	dbHost     = "changeme"
	dbPort     = "5432" // change if 5432 aint already a cool soundin' port
)

var db *sql.DB

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	clients   map[*websocket.Conn]bool
	broadcast chan string
	mutex     sync.Mutex
}

var hub = Hub{
	clients:   make(map[*websocket.Conn]bool),
	broadcast: make(chan string),
}

func (h *Hub) run() {
	for msg := range h.broadcast {
		h.mutex.Lock()
		for client := range h.clients {
			err := client.WriteMessage(websocket.TextMessage, []byte(msg))
			if err != nil {
				client.Close()
				delete(h.clients, client)
			}
		}
		h.mutex.Unlock()
	}
}

func home(c *gin.Context) {
	rows, err := db.Query(`
		SELECT id, username, content, created_at 
		FROM (SELECT * FROM messages ORDER BY created_at DESC LIMIT 500) sub 
		ORDER BY created_at ASC
	`)
	if err != nil {
		c.String(http.StatusInternalServerError, "Database error")
		return
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.Username, &m.Content, &m.CreatedAt); err != nil {
			c.String(http.StatusInternalServerError, "Scan error")
			return
		}
		messages = append(messages, m)
	}

	component := templates.Chat(messages)
	handler := templ.Handler(component)
	handler.ServeHTTP(c.Writer, c.Request)
}

func wsHandler(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	hub.mutex.Lock()
	hub.clients[conn] = true
	hub.mutex.Unlock()

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			hub.mutex.Lock()
			delete(hub.clients, conn)
			hub.mutex.Unlock()
			conn.Close()
			return
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(messageBytes, &payload); err != nil {
			continue
		}

		chatMsg, ok := payload["chat_message"].(string)
		if !ok || chatMsg == "" {
			continue
		}

		username, _ := payload["username"].(string)
		if username == "" {
			username = "Anon"
		}

		var newID int
		err = db.QueryRow("INSERT INTO messages (username, content) VALUES ($1, $2) RETURNING id", username, chatMsg).Scan(&newID)
		if err != nil {
			log.Println("Insert error:", err)
			continue
		}

		_, err = db.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
		if err != nil {
			log.Println("Cleanup error:", err)
		}

		var newMsg models.Message
		err = db.QueryRow("SELECT id, username, content, created_at FROM messages WHERE id = $1", newID).Scan(&newMsg.ID, &newMsg.Username, &newMsg.Content, &newMsg.CreatedAt)
		if err != nil {
			log.Println("Fetch new message error:", err)
			continue
		}

		component := templates.Message(newMsg)
		ctx := context.Background()
		var buf strings.Builder
		if err := component.Render(ctx, &buf); err != nil {
			log.Println("Render error:", err)
			continue
		}
		msgHTML := buf.String()

		broadcastHTML := `<div hx-swap-oob="beforeend:#messages">` + msgHTML + `</div>`
		hub.broadcast <- broadcastHTML
	}
}

func main() {
	var err error
	connStr := "host=" + dbHost + " port=" + dbPort + " user=" + dbUser + " password=" + dbPassword + " dbname=" + dbName + " sslmode=disable"
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("DB connection error:", err)
	}
	defer db.Close()

	go hub.run()

	r := gin.Default()
	r.GET("/", home)
	r.GET("/ws", wsHandler)
	r.Run(":8080")
}
