package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"temp0ral-chat/controllers"
	"temp0ral-chat/handlers"
	"temp0ral-chat/helpers"
	"temp0ral-chat/middleware"
	"temp0ral-chat/models"
	"temp0ral-chat/templates"
	"temp0ral-chat/utils"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

type Session = models.Session

func terminateIdleSessions() {
	models.SessionsMutex.Lock()
	models.ActivityMutex.RLock()

	var expiredUserIDs []string
	var expiredSessionIDs []string
	now := time.Now()
	hadTerminations := false

	for sessionID, session := range models.Sessions {
		lastActivity, exists := models.UserLastActivity[session.UserID]

		if !exists || now.Sub(lastActivity) > models.MaxIdleTime {
			expiredUserIDs = append(expiredUserIDs, session.UserID)
			expiredSessionIDs = append(expiredSessionIDs, sessionID)
			hadTerminations = true

			log.Printf("Terminating idle session for user %s (idle for %v)",
				session.UserID[:8], now.Sub(lastActivity))
		}
	}

	for _, sessionID := range expiredSessionIDs {
		delete(models.Sessions, sessionID)
	}

	models.ActivityMutex.RUnlock()
	models.SessionsMutex.Unlock()

	if len(expiredUserIDs) > 0 {
		models.ActivityMutex.Lock()
		for _, userID := range expiredUserIDs {
			delete(models.UserLastActivity, userID)
		}
		models.ActivityMutex.Unlock()

		go func(userIDs []string) {
			for _, userID := range userIDs {
				controllers.DeleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

				_, err := utils.DB.Exec("DELETE FROM messages WHERE user_id = $1", userID)
				if err != nil {
					log.Printf("Error deleting messages for idle-terminated user %s: %v", userID, err)
				} else {
					log.Printf("Deleted messages for idle-terminated user: %s", userID[:8])
				}
			}

			controllers.DeleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")
			_, err := utils.DB.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
			if err != nil {
				log.Printf("Error during message cleanup: %v", err)
			}
		}(expiredUserIDs)
	}

	if hadTerminations {
		controllers.BroadcastUserList()
	}
}

func cleanupExpiredSessions() {
	models.SessionsMutex.Lock()

	var expiredUserIDs []string
	now := time.Now()
	hadExpiredSessions := false

	for sessionID, session := range models.Sessions {
		if now.After(session.ExpiresAt) {
			expiredUserIDs = append(expiredUserIDs, session.UserID)
			delete(models.Sessions, sessionID)
			hadExpiredSessions = true
		}
	}

	models.SessionsMutex.Unlock()

	if len(expiredUserIDs) > 0 {
		models.ActivityMutex.Lock()
		for _, userID := range expiredUserIDs {
			delete(models.UserLastActivity, userID)
		}
		models.ActivityMutex.Unlock()

		go func(userIDs []string) {
			for _, userID := range userIDs {
				controllers.DeleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

				_, err := utils.DB.Exec("DELETE FROM messages WHERE user_id = $1", userID)
				if err != nil {
					log.Printf("Error deleting messages for expired user %s: %v", userID, err)
				} else {
					log.Printf("Deleted messages for expired session user: %s", userID)
				}
			}

			controllers.DeleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")

			_, err := utils.DB.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
			if err != nil {
				log.Printf("Error during message cleanup: %v", err)
			}
		}(expiredUserIDs)
	}

	if hadExpiredSessions {
		controllers.BroadcastUserList()
	}
}

func getActiveUserIDs() []string {
	models.SessionsMutex.RLock()
	defer models.SessionsMutex.RUnlock()

	var activeUserIDs []string
	now := time.Now()

	for _, session := range models.Sessions {
		if now.Before(session.ExpiresAt) {
			activeUserIDs = append(activeUserIDs, session.UserID)
		}
	}

	return activeUserIDs
}

type StatusInfo struct {
	Status       string
	LastActivity time.Time
	IsActive     bool
}

func GetUserStatusInfo(userID string) StatusInfo {
	models.ActivityMutex.RLock()
	lastActivity, exists := models.UserLastActivity[userID]
	models.ActivityMutex.RUnlock()

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

	if timeSinceActivity > models.MaxIdleTime {
		status = "idle"
		isActive = false
	} else if timeSinceActivity > models.IdleThreshold {
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
		minutesUntilTimeout := int(models.MaxIdleTime.Minutes()) - minutesIdle

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

// Configuration function to adjust idle threshold at runtime
func SetIdleThreshold(duration time.Duration) {
	// needs work
	log.Printf("Idle threshold would be set to: %v (requires restart)", duration)
}

func GetActiveUserStatsWithIdleInfo() map[string]interface{} {
	models.ActivityMutex.RLock()
	defer models.ActivityMutex.RUnlock()

	stats := map[string]interface{}{
		"total":          0,
		"online":         0,
		"idle":           0,
		"near_timeout":   0,
		"idle_threshold": models.IdleThreshold.String(),
		"max_idle_time":  models.MaxIdleTime.String(),
	}

	activeSessions := helpers.GetActiveSessions()
	stats["total"] = len(activeSessions)

	now := time.Now()
	nearTimeoutThreshold := models.MaxIdleTime - (2 * time.Minute)

	for _, session := range activeSessions {
		lastActivity, exists := models.UserLastActivity[session.UserID]
		if !exists {
			stats["online"] = stats["online"].(int) + 1
			continue
		}

		timeSinceActivity := now.Sub(lastActivity)

		if timeSinceActivity > models.IdleThreshold {
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
	ticker := time.NewTicker(models.CleanupInterval)
	go func() {
		for range ticker.C {
			terminateIdleSessions()

			cleanupExpiredSessions()

			controllers.BroadcastUserList()

			activeUserIDs := getActiveUserIDs()
			if len(activeUserIDs) > 0 {
				placeholders := make([]string, len(activeUserIDs))
				args := make([]interface{}, len(activeUserIDs))
				for i, userID := range activeUserIDs {
					placeholders[i] = "$" + string(rune('0'+i+1))
					args[i] = userID
				}

				query := "SELECT image_path FROM messages WHERE user_id NOT IN (" + strings.Join(placeholders, ",") + ") AND image_path IS NOT NULL"
				controllers.DeleteImagesForQuery(query, args...)

				delQuery := "DELETE FROM messages WHERE user_id NOT IN (" + strings.Join(placeholders, ",") + ")"
				result, err := utils.DB.Exec(delQuery, args...)
				if err != nil {
					log.Printf("Error cleaning up orphaned messages: %v", err)
				} else {
					rowsAffected, _ := result.RowsAffected()
					if rowsAffected > 0 {
						log.Printf("Cleaned up %d orphaned messages", rowsAffected)
					}
				}
			} else {
				controllers.DeleteImagesForQuery("SELECT image_path FROM messages WHERE image_path IS NOT NULL")

				result, err := utils.DB.Exec("DELETE FROM messages")
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

func logout(c *gin.Context) {
	session := c.MustGet("session").(models.Session)

	models.SessionsMutex.Lock()
	delete(models.Sessions, session.ID)
	models.SessionsMutex.Unlock()

	models.ActivityMutex.Lock()
	delete(models.UserLastActivity, session.UserID)
	models.ActivityMutex.Unlock()

	go func(userID string) {
		controllers.DeleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

		_, err := utils.DB.Exec("DELETE FROM messages WHERE user_id = $1", userID)
		if err != nil {
			log.Printf("Error deleting messages on logout: %v", err)
		} else {
			log.Printf("Deleted messages for user on logout: %s", userID)
		}
	}(session.UserID)

	controllers.BroadcastUserList()

	c.SetCookie("session_id", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

func Emojis(c *gin.Context) {
	component := templates.EmojiPicker()
	templ.Handler(component).ServeHTTP(c.Writer, c.Request)
}

func main() {
	var err error
	connStr := "host=" + models.DBHost + " port=" + models.DBPort + " user=" + models.DBUser + " password=" + models.DBPassword + " dbname=" + models.DBName + " sslmode=disable"
	utils.DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("DB connection error:", err)
	}
	defer utils.DB.Close()

	_, err = utils.DB.Exec(`
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

	_, err = utils.DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id)
	`)
	if err != nil {
		log.Fatal("Index creation error:", err)
	}

	_, err = utils.DB.Exec(`
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
	log.Printf("Started periodic session and message cleanup with idle threshold: %v", models.IdleThreshold)

	go controllers.GlobalHub.RunSocket()

	r := gin.Default()
	r.StaticFile("/chat.css", "./static/css/chat.css")
	r.StaticFile("/greeter.css", "./static/css/greeter.css")
	r.StaticFile("/chat.js", "./static/js/chat.js")
	r.StaticFile("/nibbler.png", "./static/img/nibbler.png")
	r.Static("/uploads", "./uploads")

	r.GET("/", handlers.Greeter)
	r.POST("/auth", middleware.SessionAuth)
	r.GET("/chat", middleware.AuthMiddleware(), handlers.Home)
	r.GET("/ws", middleware.AuthMiddleware(), controllers.WebSocketHandler)
	r.POST("/send-message", middleware.AuthMiddleware(), controllers.SendMessage)
	r.POST("/logout", middleware.AuthMiddleware(), logout)
	r.GET("/emojis", middleware.AuthMiddleware(), Emojis)
	r.POST("/add-emoji", middleware.AuthMiddleware(), controllers.AddEmoji)

	log.Printf("Server starting on :8080 with session duration: %v", models.SessionDuration)
	r.Run(":8080")
}
