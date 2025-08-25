package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	dbPort     = "5432"

	accessKey = "test" // Change in prod

	// Custom session cookie duration
	sessionDuration = 5 * time.Hour

	// Cleanup interval for expired sessions and messages
	cleanupInterval = 30 * time.Second
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

type Session = models.Session

var sessions = make(map[string]models.Session)
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

func generateRandomString(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func createSession() models.Session {
	sessionID := generateRandomString(16)
	userID := generateRandomString(8)

	session := models.Session{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(sessionDuration),
		CreatedAt: time.Now(),
	}

	sessionsMutex.Lock()
	sessions[sessionID] = session
	sessionsMutex.Unlock()

	return session
}

func getSession(sessionID string) (models.Session, bool) {
	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()

	session, exists := sessions[sessionID]
	if !exists {
		return models.Session{}, false
	}

	if time.Now().After(session.ExpiresAt) {
		return models.Session{}, false
	}

	return session, true
}

func cleanupExpiredSessions() {
	sessionsMutex.Lock()

	var expiredUserIDs []string
	now := time.Now()
	hadExpiredSessions := false

	for sessionID, session := range sessions {
		if now.After(session.ExpiresAt) {
			expiredUserIDs = append(expiredUserIDs, session.UserID)
			delete(sessions, sessionID)
			hadExpiredSessions = true
		}
	}

	sessionsMutex.Unlock()

	if len(expiredUserIDs) > 0 {
		go func(userIDs []string) {
			for _, userID := range userIDs {
				_, err := db.Exec("DELETE FROM messages WHERE user_id = $1", userID)
				if err != nil {
					log.Printf("Error deleting messages for expired user %s: %v", userID, err)
				} else {
					log.Printf("Deleted messages for expired session user: %s", userID)
				}
			}

			_, err := db.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
			if err != nil {
				log.Printf("Error during message cleanup: %v", err)
			}
		}(expiredUserIDs)
	}

	if hadExpiredSessions {
		broadcastUserList()
	}
}

func getActiveUserIDs() []string {
	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()

	var activeUserIDs []string
	now := time.Now()

	for _, session := range sessions {
		if now.Before(session.ExpiresAt) {
			activeUserIDs = append(activeUserIDs, session.UserID)
		}
	}

	return activeUserIDs
}

func getActiveSessions() []models.Session {
	sessionsMutex.RLock()
	defer sessionsMutex.RUnlock()

	var activeSessions []models.Session
	now := time.Now()

	for _, session := range sessions {
		if now.Before(session.ExpiresAt) {
			activeSessions = append(activeSessions, session)
		}
	}

	for i := 0; i < len(activeSessions)-1; i++ {
		for j := i + 1; j < len(activeSessions); j++ {
			if activeSessions[i].CreatedAt.After(activeSessions[j].CreatedAt) {
				activeSessions[i], activeSessions[j] = activeSessions[j], activeSessions[i]
			}
		}
	}

	return activeSessions
}

func broadcastUserList() {
	activeSessions := getActiveSessions()

	userListHTML := `<div hx-swap-oob="innerHTML:#user-list">`
	for _, session := range activeSessions {
		shortID := session.UserID[:4]
		userListHTML += `<div class="user-item" title="Session ID: ` + session.UserID + `">
			<span class="user-status"></span>
			<span class="user-id">` + shortID + `</span>
		</div>`
	}
	userListHTML += `</div>`

	countHTML := `<div hx-swap-oob="innerHTML:.user-count">` +
		fmt.Sprintf("%d online", len(activeSessions)) + `</div>`

	fullUpdate := userListHTML + countHTML

	hub.mutex.Lock()
	for client := range hub.clients {
		err := client.WriteMessage(websocket.TextMessage, []byte(fullUpdate))
		if err != nil {
			client.Close()
			delete(hub.clients, client)
		}
	}
	hub.mutex.Unlock()
}

func startPeriodicCleanup() {
	ticker := time.NewTicker(cleanupInterval)
	go func() {
		for range ticker.C {
			cleanupExpiredSessions()

			activeUserIDs := getActiveUserIDs()
			if len(activeUserIDs) > 0 {
				placeholders := make([]string, len(activeUserIDs))
				args := make([]interface{}, len(activeUserIDs))
				for i, userID := range activeUserIDs {
					placeholders[i] = "$" + string(rune('0'+i+1))
					args[i] = userID
				}

				query := "DELETE FROM messages WHERE user_id NOT IN (" + strings.Join(placeholders, ",") + ")"
				result, err := db.Exec(query, args...)
				if err != nil {
					log.Printf("Error cleaning up orphaned messages: %v", err)
				} else {
					rowsAffected, _ := result.RowsAffected()
					if rowsAffected > 0 {
						log.Printf("Cleaned up %d orphaned messages", rowsAffected)
					}
				}
			} else {
				result, err := db.Exec("DELETE FROM messages")
				if err != nil {
					log.Printf("Error clearing all messages: %v", err)
				} else {
					rowsAffected, _ := result.RowsAffected()
					if rowsAffected > 0 {
						log.Printf("Cleared all messages due to no active sessions: %d messages", rowsAffected)
					}
				}
			}
		}
	}()
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session_id")
		if err != nil {
			c.Redirect(http.StatusFound, "/?error=no_session")
			c.Abort()
			return
		}

		session, exists := getSession(cookie)
		if !exists {
			c.SetCookie("session_id", "", -1, "/", "", false, true)
			c.Redirect(http.StatusFound, "/?error=session_expired")
			c.Abort()
			return
		}

		c.Set("session", session)
		c.Next()
	}
}

func greeter(c *gin.Context) {
	errorMsg := c.Query("error")
	component := templates.Greeter(errorMsg)
	handler := templ.Handler(component)
	handler.ServeHTTP(c.Writer, c.Request)
}

func authenticate(c *gin.Context) {
	providedKey := c.PostForm("access_key")

	if providedKey != accessKey {
		c.Redirect(http.StatusFound, "/?error=invalid_key")
		return
	}

	session := createSession()

	c.SetCookie("session_id", session.ID, int(sessionDuration.Seconds()), "/", "", false, true)

	c.Redirect(http.StatusFound, "/chat")
}

func home(c *gin.Context) {
	session, _ := c.Get("session")
	userSession := session.(models.Session)

	activeSessions := getActiveSessions()
	activeUserIDs := make([]string, len(activeSessions))
	for i, sess := range activeSessions {
		activeUserIDs[i] = sess.UserID
	}

	if len(activeUserIDs) == 0 {
		component := templates.Chat([]models.Message{}, userSession.UserID, activeSessions)
		handler := templ.Handler(component)
		handler.ServeHTTP(c.Writer, c.Request)
		return
	}

	placeholders := make([]string, len(activeUserIDs))
	args := make([]interface{}, len(activeUserIDs))
	for i, userID := range activeUserIDs {
		placeholders[i] = "$" + string(rune('0'+i+1))
		args[i] = userID
	}

	query := `
		SELECT id, username, content, created_at, user_id
		FROM (
			SELECT * FROM messages 
			WHERE user_id IN (` + strings.Join(placeholders, ",") + `)
			ORDER BY created_at DESC LIMIT 500
		) sub 
		ORDER BY created_at ASC
	`

	rows, err := db.Query(query, args...)
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

	component := templates.Chat(messages, userSession.UserID, activeSessions)
	handler := templ.Handler(component)
	handler.ServeHTTP(c.Writer, c.Request)
}

func wsHandler(c *gin.Context) {
	session, exists := c.Get("session")
	if !exists {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}

	userSession := session.(models.Session)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	hub.mutex.Lock()
	hub.clients[conn] = true
	hub.mutex.Unlock()

	broadcastUserList()

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			hub.mutex.Lock()
			delete(hub.clients, conn)
			hub.mutex.Unlock()
			conn.Close()
			broadcastUserList()
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

		if _, valid := getSession(userSession.ID); !valid {
			errorHTML := `<div hx-swap-oob="innerHTML:#error-container">
				<div class="error-message">
					Your session has expired. Please <a href="/" class="error-link">refresh the page</a> to continue.
				</div>
			</div>`

			err := conn.WriteMessage(websocket.TextMessage, []byte(errorHTML))
			if err != nil {
				log.Println("Error sending session expired message:", err)
			}

			time.Sleep(100 * time.Millisecond)
			conn.Close()
			return
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

	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id)
	`)
	if err != nil {
		log.Fatal("Index creation error:", err)
	}

	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at DESC)
	`)
	if err != nil {
		log.Fatal("Index creation error:", err)
	}

	startPeriodicCleanup()
	log.Println("Started periodic session and message cleanup")

	go hub.run()

	r := gin.Default()
	r.GET("/", greeter)
	r.POST("/auth", authenticate)
	r.GET("/chat", authMiddleware(), home)
	r.GET("/ws", authMiddleware(), wsHandler)

	log.Printf("Server starting on :8080 with session duration: %v", sessionDuration)
	r.Run(":8080")
}
