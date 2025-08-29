package main

import (
	"log"

	"temp0ral-chat/controllers"
	"temp0ral-chat/models"
	"temp0ral-chat/routes"
	"temp0ral-chat/utils"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

/*
type Session = models.Session

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

func StatusCheck(userID string) (string, string) {
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
*/

func main() {
	if err := utils.SetupDatabase(); err != nil {
		log.Fatalf("Failed to setup database: %v", err)
	}
	defer func() {
		if err := utils.CloseDatabase(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()

	controllers.StartPeriodicCleanup()
	log.Printf("Started periodic session and message cleanup with idle threshold: %v", models.IdleThreshold)

	go controllers.GlobalHub.RunSocket()

	r := gin.Default()
	routes.Temp0ralRouter(r)

	log.Printf("Server starting on :8080 with session duration: %v", models.SessionDuration)
	r.Run(":8080")
}
