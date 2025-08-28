package helpers

import (
	"temp0ral-chat/models"
	"time"
)

func GetActiveSessions() []models.Session {
	models.SessionsMutex.RLock()
	defer models.SessionsMutex.RUnlock()

	var activeSessions []models.Session
	now := time.Now()

	for _, session := range models.Sessions {
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

func GetActiveUserIDs() []string {
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
