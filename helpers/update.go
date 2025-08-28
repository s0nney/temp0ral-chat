package helpers

import (
	"temp0ral-chat/models"
	"time"
)

func UpdateUserActivity(userID string) {
	models.ActivityMutex.Lock()
	models.UserLastActivity[userID] = time.Now()
	models.ActivityMutex.Unlock()
}
