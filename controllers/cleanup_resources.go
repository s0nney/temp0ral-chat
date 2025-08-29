package controllers

import (
	"log"
	"strings"
	"temp0ral-chat/models"
	"temp0ral-chat/utils"
	"time"
)

func StartPeriodicCleanup() {
	ticker := time.NewTicker(models.CleanupInterval)
	go func() {
		for range ticker.C {
			TerminateIdleSessions()

			CleanupExpiredSessions()

			BroadcastUserList()

			activeUserIDs := ActiveIDs()
			if len(activeUserIDs) > 0 {
				placeholders := make([]string, len(activeUserIDs))
				args := make([]interface{}, len(activeUserIDs))
				for i, userID := range activeUserIDs {
					placeholders[i] = "$" + string(rune('0'+i+1))
					args[i] = userID
				}

				query := "SELECT image_path FROM messages WHERE user_id NOT IN (" + strings.Join(placeholders, ",") + ") AND image_path IS NOT NULL"
				DeleteImagesForQuery(query, args...)

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
				DeleteImagesForQuery("SELECT image_path FROM messages WHERE image_path IS NOT NULL")

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
