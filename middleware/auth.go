package middleware

import (
	"net/http"
	"temp0ral-chat/controllers"
	"temp0ral-chat/models"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session_id")
		if err != nil {
			c.Redirect(http.StatusFound, "/?error=no_session")
			c.Abort()
			return
		}

		session, exists := controllers.GetSession(cookie)
		if !exists {
			c.SetCookie("session_id", "", -1, "/", "", false, true)
			c.Redirect(http.StatusFound, "/?error=session_expired")
			c.Abort()
			return
		}

		c.Set("session", session)
		c.Next()
	}
}

func SessionAuth(c *gin.Context) {
	providedKey := c.PostForm("access_key")

	if providedKey != models.AccessKey {
		c.Redirect(http.StatusFound, "/?error=invalid_key")
		return
	}

	session := controllers.CreateSession()

	c.SetCookie("session_id", session.ID, int(models.SessionDuration.Seconds()), "/", "", false, true)

	c.Redirect(http.StatusFound, "/chat")
}
