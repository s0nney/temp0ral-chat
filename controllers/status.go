package controllers

import (
	"temp0ral-chat/helpers"
	"temp0ral-chat/models"
	"time"
)

func GetState() map[string]interface{} {
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

func ActiveIDs() []string {
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
