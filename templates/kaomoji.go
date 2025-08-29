package templates

import (
	"html"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

func AddEmoji(c *gin.Context) {
	chatMsg := c.PostForm("chat_message")
	emoji := c.PostForm("emoji")
	newContent := chatMsg + emoji

	c.Header("Content-Type", "text/html")
	c.String(200, `<input name="chat_message" id="message-input" placeholder="Type your message..." autocomplete="off" value="%s">`, html.EscapeString(newContent))
}

func Emojis(c *gin.Context) {
	component := EmojiPicker()
	templ.Handler(component).ServeHTTP(c.Writer, c.Request)
}
