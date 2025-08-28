package controllers

import (
	"context"
	"database/sql"
	"image"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"temp0ral-chat/helpers"
	"temp0ral-chat/models"
	"temp0ral-chat/templates"
	"temp0ral-chat/utils"

	"github.com/gin-gonic/gin"
)

func SendMessage(c *gin.Context) {
	sessionAny, exists := c.Get("session")
	if !exists {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	userSession := sessionAny.(models.Session)

	if _, valid := GetSession(userSession.ID); !valid {
		c.String(http.StatusUnauthorized, "Session expired")
		return
	}

	helpers.UpdateUserActivity(userSession.UserID)

	username := c.PostForm("username")
	if username == "" {
		username = "Anon"
	}

	chatMsg := c.PostForm("chat_message")

	var imagePath string
	file, err := c.FormFile("image")
	if err == nil {
		if file.Size > models.MaxUploadSize {
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
		filename := helpers.GenerateID(16) + ext
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

	err = utils.DB.QueryRow(
		"INSERT INTO messages (username, content, user_id, image_path) VALUES ($1, $2, $3, $4) RETURNING id",
		username, chatMsg, userSession.UserID, dbImagePath,
	).Scan(&newID)
	if err != nil {
		log.Println("Insert error:", err)
		c.String(http.StatusInternalServerError, "Database error")
		return
	}

	DeleteImagesForQuery("SELECT image_path FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500) AND image_path IS NOT NULL")

	_, err = utils.DB.Exec("DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC LIMIT 500)")
	if err != nil {
		log.Println("Cleanup error:", err)
	}

	var newMsg models.Message
	var imgPath sql.NullString
	err = utils.DB.QueryRow("SELECT id, username, content, created_at, user_id, image_path FROM messages WHERE id = $1", newID).Scan(
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
	GlobalHub.broadcast <- broadcastHTML

	BroadcastUserList()

	clearResponse := `
		<input id="message-input" name="chat_message" placeholder="Type your message..." autocomplete="off" value="" hx-swap-oob="true">
		<input type="file" id="file-input" name="image" accept="image/*" style="display: none;" hx-swap-oob="true">
		<div id="file-preview" hx-swap-oob="outerHTML"></div>
		<div id="emoji-picker" hx-swap-oob="innerHTML"></div>
	`

	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, clearResponse)
}
