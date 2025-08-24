package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"temp0ral-chat/models"
	"temp0ral-chat/templates"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
)

const (
	dbUser     = ""
	dbPassword = ""
	dbName     = ""
	dbHost     = ""
	dbPort     = "5432" // default port is already goated but do as thou wilt ig

	accessKey = "secret" // Change in production bruh

	sessionDuration = 24 * time.Hour
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

type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

// use database or redis in production!
var sessions = make(map[string]Session)
var sessionsMutex sync.RWMutex

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

func generateID(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func createSession() Session {
	sessionID := generateID(16)
	userID := generateID(8)

	session := Session{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(sessionDuration),
	}

	sessionsMutex.Lock()
	sessions[sessionID] = session
	sessionsMutex.Unlock()

	return session
}

func getSession(sessionID string) (Session, bool) {
	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()

	session, exists := sessions[sessionID]
	if !exists {
		return Session{}, false
	}

	if time.Now().After(session.ExpiresAt) {
		sessionsMutex.RUnlock()
		sessionsMutex.Lock()
		delete(sessions, sessionID)
		sessionsMutex.Unlock()
		sessionsMutex.RLock()
		return Session{}, false
	}

	return session, true
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session_id")
		if err != nil {
			c.Redirect(http.StatusFound, "/")
			c.Abort()
			return
		}

		session, exists := getSession(cookie)
		if !exists {
			c.SetCookie("session_id", "", -1, "/", "", false, true)
			c.Redirect(http.StatusFound, "/")
			c.Abort()
			return
		}

		c.Set("session", session)
		c.Next()
	}
}

func greeter(c *gin.Context) {
	component := templates.Greeter()
	handler := templ.Handler(component)
	handler.ServeHTTP(c.Writer, c.Request)
}

func authenticate(c *gin.Context) {
	providedKey := c.PostForm("access_key")

	if providedKey != accessKey {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid access key"})
		return
	}

	session := createSession()

	c.SetCookie("session_id", session.ID, int(sessionDuration.Seconds()), "/", "", false, true)

	c.Redirect(http.StatusFound, "/chat")
}

func home(c *gin.Context) {
	session, _ := c.Get("session")
	userSession := session.(Session)

	rows, err := db.Query(`
		SELECT id, username, content, created_at, user_id
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
		if err := rows.Scan(&m.ID, &m.Username, &m.Content, &m.CreatedAt, &m.UserID); err != nil {
			c.String(http.StatusInternalServerError, "Scan error")
			return
		}
		messages = append(messages, m)
	}

	component := templates.Chat(messages, userSession.UserID)
	handler := templ.Handler(component)
	handler.ServeHTTP(c.Writer, c.Request)
}

func wsHandler(c *gin.Context) {
	session, exists := c.Get("session")
	if !exists {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}

	userSession := session.(Session)

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
		err = db.QueryRow("INSERT INTO messages (username, content, user_id) VALUES ($1, $2, $3) RETURNING id", username, chatMsg, userSession.UserID).Scan(&newID)
		if err != nil {
			log.Println("Insert error:", err)
			continue
		}

		_, err = db.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
		if err != nil {
			log.Println("Cleanup error:", err)
		}

		var newMsg models.Message
		err = db.QueryRow("SELECT id, username, content, created_at, user_id FROM messages WHERE id = $1", newID).Scan(&newMsg.ID, &newMsg.Username, &newMsg.Content, &newMsg.CreatedAt, &newMsg.UserID)
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

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id SERIAL PRIMARY KEY,
			username VARCHAR(255) NOT NULL,
			content TEXT NOT NULL,
			user_id VARCHAR(255) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal("Table creation error:", err)
	}

	go hub.run()

	r := gin.Default()
	r.GET("/", greeter)
	r.POST("/auth", authenticate)
	r.GET("/chat", authMiddleware(), home)
	r.GET("/ws", authMiddleware(), wsHandler)
	r.Run(":8080")
}
