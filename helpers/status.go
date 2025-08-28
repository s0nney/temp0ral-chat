package helpers

import (
	"temp0ral-chat/models"
	"time"
)

func GetUserStatus(userID string) string {
	models.ActivityMutex.RLock()
	lastActivity, exists := models.UserLastActivity[userID]
	models.ActivityMutex.RUnlock()

	if !exists {
		return "online"
	}

	if time.Since(lastActivity) > models.IdleThreshold {
		return "idle"
	}

	return "online"
}
