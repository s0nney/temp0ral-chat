package controllers

import (
	"log"
	"temp0ral-chat/models"
	"temp0ral-chat/utils"
	"time"
)

func CleanupExpiredSessions() {
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
				DeleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

				_, err := utils.DB.Exec("DELETE FROM messages WHERE user_id = $1", userID)
				if err != nil {
					log.Printf("Error deleting messages for expired user %s: %v", userID, err)
				} else {
					log.Printf("Deleted messages for expired session user: %s", userID)
				}
			}

			DeleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")

			_, err := utils.DB.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
			if err != nil {
				log.Printf("Error during message cleanup: %v", err)
			}
		}(expiredUserIDs)
	}

	if hadExpiredSessions {
		BroadcastUserList()
	}
}

func TerminateIdleSessions() {
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
				DeleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

				_, err := utils.DB.Exec("DELETE FROM messages WHERE user_id = $1", userID)
				if err != nil {
					log.Printf("Error deleting messages for idle-terminated user %s: %v", userID, err)
				} else {
					log.Printf("Deleted messages for idle-terminated user: %s", userID[:8])
				}
			}

			DeleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")
			_, err := utils.DB.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
			if err != nil {
				log.Printf("Error during message cleanup: %v", err)
			}
		}(expiredUserIDs)
	}

	if hadTerminations {
		BroadcastUserList()
	}
}
