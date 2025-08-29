package controllers

import (
	"log"
	"net/http"
	"temp0ral-chat/models"
	"temp0ral-chat/utils"

	"github.com/gin-gonic/gin"
)

func Logout(c *gin.Context) {
	session := c.MustGet("session").(models.Session)

	models.SessionsMutex.Lock()
	delete(models.Sessions, session.ID)
	models.SessionsMutex.Unlock()

	models.ActivityMutex.Lock()
	delete(models.UserLastActivity, session.UserID)
	models.ActivityMutex.Unlock()

	go func(userID string) {
		DeleteImagesForQuery("SELECT image_path FROM messages WHERE user_id = $1 AND image_path IS NOT NULL", userID)

		_, err := utils.DB.Exec("DELETE FROM messages WHERE user_id = $1", userID)
		if err != nil {
			log.Printf("Error deleting messages on logout: %v", err)
		} else {
			log.Printf("Deleted messages for user on logout: %s", userID)
		}
	}(session.UserID)

	BroadcastUserList()

	c.SetCookie("session_id", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}
