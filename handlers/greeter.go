package handlers

import (
	"temp0ral-chat/templates"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

func Greeter(c *gin.Context) {
	errorMsg := c.Query("error")
	component := templates.Greeter(errorMsg)
	handler := templ.Handler(component)
	handler.ServeHTTP(c.Writer, c.Request)
}
