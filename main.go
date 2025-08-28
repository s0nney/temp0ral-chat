package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"temp0ral-chat/handlers"
	"temp0ral-chat/models"
	"temp0ral-chat/templates"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
)

const (
	dbUser          = ""
	dbPassword      = ""
	dbName          = ""
	dbHost          = ""
	dbPort          = "5432"
	accessKey       = "test" // change in prod!
	sessionDuration = 5 * time.Hour
	cleanupInterval = 30 * time.Second
	maxUploadSize   = 5 * 1024 * 1024  // 5 MB upload limit!
	idleThreshold   = 3 * time.Second  // Users idle after N seconds of no activity
	maxIdleTime     = 60 * time.Second // Terminate sessions after N minutes of inactivity
)

var db *sql.DB

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

type Session = models.Session

var sessions = make(map[string]models.Session)
var sessionsMutex sync.RWMutex

var userLastActivity = make(map[string]time.Time)
var activityMutex sync.RWMutex

var hub = Hub{
	clients:   make(map[*Client]bool),
	broadcast: make(chan string),
}

func (h *Hub) run() {
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

func generateID(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func createSession() models.Session {
	sessionID := generateID(16)
	userID := generateID(8)

	session := models.Session{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(sessionDuration),
		CreatedAt: time.Now(),
	}

	sessionsMutex.Lock()
	sessions[sessionID] = session
	sessionsMutex.Unlock()

	updateUserActivity(userID)

	return session
}

func terminateIdleSessions() {
	sessionsMutex.Lock()
	activityMutex.RLock()

	var expiredUserIDs []string
	var expiredSessionIDs []string
	now := time.Now()
	hadTerminations := false

	for sessionID, session := range sessions {
		lastActivity, exists := userLastActivity[session.UserID]

		if !exists || now.Sub(lastActivity) > maxIdleTime {
			expiredUserIDs = append(expiredUserIDs, session.UserID)
			expiredSessionIDs = append(expiredSessionIDs, sessionID)
			hadTerminations = true

			log.Printf("Terminating idle session for user %s (idle for %v)",
				session.UserID[:8], now.Sub(lastActivity))
		}
	}

	for _, sessionID := range expiredSessionIDs {
		delete(sessions, sessionID)
	}

	activityMutex.RUnlock()
	sessionsMutex.Unlock()

	if len(expiredUserIDs) > 0 {
		activityMutex.Lock()
		for _, userID := range expiredUserIDs {
			delete(userLastActivity, userID)
		}
		activityMutex.Unlock()

		go func(userIDs []string) {
			for _, userID := range userIDs {
				deleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

				_, err := db.Exec("DELETE FROM messages WHERE user_id = $1", userID)
				if err != nil {
					log.Printf("Error deleting messages for idle-terminated user %s: %v", userID, err)
				} else {
					log.Printf("Deleted messages for idle-terminated user: %s", userID[:8])
				}
			}

			deleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")
			_, err := db.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
			if err != nil {
				log.Printf("Error during message cleanup: %v", err)
			}
		}(expiredUserIDs)
	}

	if hadTerminations {
		broadcastUserList()
	}
}

func updateUserActivity(userID string) {
	activityMutex.Lock()
	userLastActivity[userID] = time.Now()
	activityMutex.Unlock()
}

func getUserStatus(userID string) string {
	activityMutex.RLock()
	lastActivity, exists := userLastActivity[userID]
	activityMutex.RUnlock()

	if !exists {
		return "online"
	}

	if time.Since(lastActivity) > idleThreshold {
		return "idle"
	}

	return "online"
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

func deleteImagesForQuery(query string, args ...interface{}) {
	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("Error querying images to delete: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var imagePath sql.NullString
		if err := rows.Scan(&imagePath); err != nil {
			log.Printf("Error scanning image path: %v", err)
			continue
		}
		if imagePath.Valid && imagePath.String != "" {
			err := os.Remove("." + imagePath.String)
			if err != nil {
				log.Printf("Error deleting image file %s: %v", imagePath.String, err)
			} else {
				log.Printf("Deleted image file: %s", imagePath.String)
			}
		}
	}
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
		activityMutex.Lock()
		for _, userID := range expiredUserIDs {
			delete(userLastActivity, userID)
		}
		activityMutex.Unlock()

		go func(userIDs []string) {
			for _, userID := range userIDs {
				deleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

				_, err := db.Exec("DELETE FROM messages WHERE user_id = $1", userID)
				if err != nil {
					log.Printf("Error deleting messages for expired user %s: %v", userID, err)
				} else {
					log.Printf("Deleted messages for expired session user: %s", userID)
				}
			}

			deleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")

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

func AuthMiddleware() gin.HandlerFunc {
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
		shortID := session.UserID[:8]
		status := getUserStatus(session.UserID)
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

	hub.mutex.Lock()
	for client := range hub.clients {
		client.mutex.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, []byte(fullUpdate))
		client.mutex.Unlock()
		if err != nil {
			client.conn.Close()
			hub.mutex.Lock()
			delete(hub.clients, client)
			hub.mutex.Unlock()
		}
	}
	hub.mutex.Unlock()
}

type StatusInfo struct {
	Status       string
	LastActivity time.Time
	IsActive     bool
}

func GetUserStatusInfo(userID string) StatusInfo {
	activityMutex.RLock()
	lastActivity, exists := userLastActivity[userID]
	activityMutex.RUnlock()

	if !exists {
		return StatusInfo{
			Status:       "online",
			LastActivity: time.Now(),
			IsActive:     true,
		}
	}

	timeSinceActivity := time.Since(lastActivity)

	var status string
	var isActive bool

	if timeSinceActivity > maxIdleTime {
		status = "idle"
		isActive = false
	} else if timeSinceActivity > idleThreshold {
		status = "idle"
		isActive = false
	} else {
		status = "online"
		isActive = true
	}

	return StatusInfo{
		Status:       status,
		LastActivity: lastActivity,
		IsActive:     isActive,
	}
}

func GetUserStatusWithTooltip(userID string) (string, string) {
	info := GetUserStatusInfo(userID)
	timeSinceActivity := time.Since(info.LastActivity)

	var tooltip string
	switch info.Status {
	case "idle":
		minutesIdle := int(timeSinceActivity.Minutes())
		minutesUntilTimeout := int(maxIdleTime.Minutes()) - minutesIdle

		if minutesUntilTimeout <= 0 {
			tooltip = "Session will terminate soon"
		} else if minutesUntilTimeout <= 2 {
			tooltip = fmt.Sprintf("Idle for %d min (expires in %d min)", minutesIdle, minutesUntilTimeout)
		} else {
			tooltip = fmt.Sprintf("Idle for %d minutes", minutesIdle)
		}
	case "online":
		tooltip = "Active now"
	default:
		tooltip = "Unknown status"
	}

	return info.Status, tooltip
}

func SetIdleTimeouts(idleThreshold, maxIdle time.Duration) {
	log.Printf("Current idle threshold: %v, max idle time: %v", idleThreshold, maxIdle)
	log.Printf("Note: Changing these values requires modifying constants and restarting the application")
}

/*
func broadcastUserListWithTooltips() {
	activeSessions := getActiveSessions()

	userListHTML := `<div hx-swap-oob="innerHTML:#user-list">`
	for _, session := range activeSessions {
		shortID := session.UserID[:8]
		status, tooltip := GetUserStatusWithTooltip(session.UserID)
		statusClass := "user-status-" + status

		userListHTML += `<div class="user-item" title="Session ID: ` + session.UserID + ` | ` + tooltip + `" data-status-info="` + tooltip + `">
			<span class="user-status ` + statusClass + `"></span>
			<span class="user-id">` + shortID + `</span>
		</div>`
	}
	userListHTML += `</div>`

	countHTML := `<div hx-swap-oob="innerHTML:.user-count">` +
		fmt.Sprintf("%d online", len(activeSessions)) + `</div>`

	fullUpdate := userListHTML + countHTML

	hub.mutex.Lock()
	for client := range hub.clients {
		client.mutex.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, []byte(fullUpdate))
		client.mutex.Unlock()
		if err != nil {
			client.conn.Close()
			hub.mutex.Lock()
			delete(hub.clients, client)
			hub.mutex.Unlock()
		}
	}
	hub.mutex.Unlock()
}
*/

// Configuration function to adjust idle threshold at runtime
func SetIdleThreshold(duration time.Duration) {
	// needs work
	log.Printf("Idle threshold would be set to: %v (requires restart)", duration)
}

func GetActiveUserStatsWithIdleInfo() map[string]interface{} {
	activityMutex.RLock()
	defer activityMutex.RUnlock()

	stats := map[string]interface{}{
		"total":          0,
		"online":         0,
		"idle":           0,
		"near_timeout":   0,
		"idle_threshold": idleThreshold.String(),
		"max_idle_time":  maxIdleTime.String(),
	}

	activeSessions := getActiveSessions()
	stats["total"] = len(activeSessions)

	now := time.Now()
	nearTimeoutThreshold := maxIdleTime - (2 * time.Minute)

	for _, session := range activeSessions {
		lastActivity, exists := userLastActivity[session.UserID]
		if !exists {
			stats["online"] = stats["online"].(int) + 1
			continue
		}

		timeSinceActivity := now.Sub(lastActivity)

		if timeSinceActivity > idleThreshold {
			stats["idle"] = stats["idle"].(int) + 1

			if timeSinceActivity > nearTimeoutThreshold {
				stats["near_timeout"] = stats["near_timeout"].(int) + 1
			}
		} else {
			stats["online"] = stats["online"].(int) + 1
		}
	}

	return stats
}

func startPeriodicCleanup() {
	ticker := time.NewTicker(cleanupInterval)
	go func() {
		for range ticker.C {
			terminateIdleSessions()

			cleanupExpiredSessions()

			broadcastUserList()

			activeUserIDs := getActiveUserIDs()
			if len(activeUserIDs) > 0 {
				placeholders := make([]string, len(activeUserIDs))
				args := make([]interface{}, len(activeUserIDs))
				for i, userID := range activeUserIDs {
					placeholders[i] = "$" + string(rune('0'+i+1))
					args[i] = userID
				}

				query := "SELECT image_path FROM messages WHERE user_id NOT IN (" + strings.Join(placeholders, ",") + ") AND image_path IS NOT NULL"
				deleteImagesForQuery(query, args...)

				delQuery := "DELETE FROM messages WHERE user_id NOT IN (" + strings.Join(placeholders, ",") + ")"
				result, err := db.Exec(delQuery, args...)
				if err != nil {
					log.Printf("Error cleaning up orphaned messages: %v", err)
				} else {
					rowsAffected, _ := result.RowsAffected()
					if rowsAffected > 0 {
						log.Printf("Cleaned up %d orphaned messages", rowsAffected)
					}
				}
			} else {
				deleteImagesForQuery("SELECT image_path FROM messages WHERE image_path IS NOT NULL")

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

	updateUserActivity(userSession.UserID)

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
		SELECT id, username, content, created_at, user_id, image_path
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
		var imagePath sql.NullString
		if err := rows.Scan(&m.ID, &m.Username, &m.Content, &m.CreatedAt, &m.UserID, &imagePath); err != nil {
			c.String(http.StatusInternalServerError, "Scan error")
			return
		}
		if imagePath.Valid {
			m.ImagePath = imagePath.String
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

	updateUserActivity(userSession.UserID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{conn: conn}
	hub.mutex.Lock()
	hub.clients[client] = true
	hub.mutex.Unlock()

	broadcastUserList()

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			client.mutex.Lock()
			client.conn.Close()
			client.mutex.Unlock()
			hub.mutex.Lock()
			delete(hub.clients, client)
			hub.mutex.Unlock()
			broadcastUserList()
			return
		}

		_ = messageBytes

		if _, valid := getSession(userSession.ID); !valid {
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

func sendMessage(c *gin.Context) {
	sessionAny, exists := c.Get("session")
	if !exists {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	userSession := sessionAny.(models.Session)

	if _, valid := getSession(userSession.ID); !valid {
		c.String(http.StatusUnauthorized, "Session expired")
		return
	}

	updateUserActivity(userSession.UserID)

	username := c.PostForm("username")
	if username == "" {
		username = "Anon"
	}

	chatMsg := c.PostForm("chat_message")

	var imagePath string
	file, err := c.FormFile("image")
	if err == nil {
		if file.Size > maxUploadSize {
			c.String(http.StatusBadRequest, "File too large (max 5MB)")
			return
		}

		f, err := file.Open()
		if err != nil {
			c.String(http.StatusInternalServerError, "Error opening file")
			return
		}
		defer f.Close()

		_, _, err = image.DecodeConfig(f)
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid image format")
			return
		}

		f.Seek(0, io.SeekStart)

		ext := filepath.Ext(file.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		filename := generateID(16) + ext
		imagePath = "/uploads/" + filename

		if err := c.SaveUploadedFile(file, "."+imagePath); err != nil {
			log.Println("Save file error:", err)
			c.String(http.StatusInternalServerError, "Error saving file")
			return
		}
	}

	if chatMsg == "" && imagePath == "" {
		c.String(http.StatusBadRequest, "Message cannot be empty")
		return
	}

	var newID int
	var dbImagePath interface{}
	if imagePath != "" {
		dbImagePath = imagePath
	} else {
		dbImagePath = nil
	}

	err = db.QueryRow(
		"INSERT INTO messages (username, content, user_id, image_path) VALUES ($1, $2, $3, $4) RETURNING id",
		username, chatMsg, userSession.UserID, dbImagePath,
	).Scan(&newID)
	if err != nil {
		log.Println("Insert error:", err)
		c.String(http.StatusInternalServerError, "Database error")
		return
	}

	deleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")

	_, err = db.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
	if err != nil {
		log.Println("Cleanup error:", err)
	}

	var newMsg models.Message
	var imgPath sql.NullString
	err = db.QueryRow("SELECT id, username, content, created_at, user_id, image_path FROM messages WHERE id = $1", newID).Scan(
		&newMsg.ID, &newMsg.Username, &newMsg.Content, &newMsg.CreatedAt, &newMsg.UserID, &imgPath,
	)
	if err != nil {
		log.Println("Fetch new message error:", err)
		c.String(http.StatusInternalServerError, "Database error")
		return
	}
	if imgPath.Valid {
		newMsg.ImagePath = imgPath.String
	}

	component := templates.Message(newMsg)
	ctx := context.Background()
	var buf strings.Builder
	if err := component.Render(ctx, &buf); err != nil {
		log.Println("Render error:", err)
		c.String(http.StatusInternalServerError, "Render error")
		return
	}
	msgHTML := buf.String()

	broadcastHTML := `<div hx-swap-oob="beforeend:#messages">` + msgHTML + `</div>`
	hub.broadcast <- broadcastHTML

	broadcastUserList()

	clearResponse := `
		<input id="message-input" name="chat_message" placeholder="Type your message..." autocomplete="off" value="" hx-swap-oob="true">
		<input type="file" id="file-input" name="image" accept="image/*" style="display: none;" hx-swap-oob="true">
		<div id="file-preview" hx-swap-oob="outerHTML"></div>
		<div id="emoji-picker" hx-swap-oob="innerHTML"></div>
	`

	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, clearResponse)
}

func logout(c *gin.Context) {
	session := c.MustGet("session").(models.Session)

	sessionsMutex.Lock()
	delete(sessions, session.ID)
	sessionsMutex.Unlock()

	activityMutex.Lock()
	delete(userLastActivity, session.UserID)
	activityMutex.Unlock()

	go func(userID string) {
		deleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

		_, err := db.Exec("DELETE FROM messages WHERE user_id = $1", userID)
		if err != nil {
			log.Printf("Error deleting messages on logout: %v", err)
		} else {
			log.Printf("Deleted messages for user on logout: %s", userID)
		}
	}(session.UserID)

	broadcastUserList()

	c.SetCookie("session_id", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

func emojis(c *gin.Context) {
	component := templates.EmojiPicker()
	templ.Handler(component).ServeHTTP(c.Writer, c.Request)
}

func addEmoji(c *gin.Context) {
	chatMsg := c.PostForm("chat_message")
	emoji := c.PostForm("emoji")
	newContent := chatMsg + emoji

	c.Header("Content-Type", "text/html")
	c.String(200, `<input name="chat_message" id="message-input" placeholder="Type your message..." autocomplete="off" value="%s">`, html.EscapeString(newContent))
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
			image_path VARCHAR(255),
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

	err = os.MkdirAll("./uploads", 0755)
	if err != nil {
		log.Fatal("Error creating uploads directory:", err)
	}

	startPeriodicCleanup()
	log.Printf("Started periodic session and message cleanup with idle threshold: %v", idleThreshold)

	go hub.run()

	r := gin.Default()
	r.StaticFile("/chat.css", "./static/css/chat.css")
	r.StaticFile("/greeter.css", "./static/css/greeter.css")
	r.StaticFile("/chat.js", "./static/js/chat.js")
	r.StaticFile("/nibbler.png", "./static/img/nibbler.png")
	r.Static("/uploads", "./uploads")

	r.GET("/", handlers.Greeter)
	r.POST("/auth", authenticate)
	r.GET("/chat", AuthMiddleware(), home)
	r.GET("/ws", AuthMiddleware(), wsHandler)
	r.POST("/send-message", AuthMiddleware(), sendMessage)
	r.POST("/logout", AuthMiddleware(), logout)
	r.GET("/emojis", AuthMiddleware(), emojis)
	r.POST("/add-emoji", AuthMiddleware(), addEmoji)

	log.Printf("Server starting on :8080 with session duration: %v", sessionDuration)
	r.Run(":8080")
}
