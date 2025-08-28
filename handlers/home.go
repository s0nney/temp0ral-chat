package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"temp0ral-chat/helpers"
	"temp0ral-chat/models"
	"temp0ral-chat/templates"
	"temp0ral-chat/utils"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

func Home(c *gin.Context) {
	session, _ := c.Get("session")
	userSession := session.(models.Session)

	helpers.UpdateUserActivity(userSession.UserID)

	activeSessions := helpers.GetActiveSessions()
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

	rows, err := utils.DB.Query(query, args...)
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
