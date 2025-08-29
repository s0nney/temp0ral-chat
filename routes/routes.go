package routes

import (
	"temp0ral-chat/controllers"
	"temp0ral-chat/handlers"
	"temp0ral-chat/middleware"
	"temp0ral-chat/templates"

	"github.com/gin-gonic/gin"
)

func Temp0ralRouter(r *gin.Engine) {
	r.StaticFile("/chat.css", "./static/css/chat.css")
	r.StaticFile("/greeter.css", "./static/css/greeter.css")
	r.StaticFile("/chat.js", "./static/js/chat.js")
	r.Static("/uploads", "./uploads")

	r.GET("/", handlers.Greeter)
	r.POST("/auth", middleware.SessionAuth)
	r.GET("/chat", middleware.AuthMiddleware(), handlers.Home)
	r.GET("/ws", middleware.AuthMiddleware(), controllers.WebSocketHandler)
	r.POST("/send-message", middleware.AuthMiddleware(), controllers.SendMessage)
	r.POST("/logout", middleware.AuthMiddleware(), controllers.Logout)
	r.GET("/emojis", middleware.AuthMiddleware(), templates.Emojis)
	r.POST("/add-emoji", middleware.AuthMiddleware(), templates.AddEmoji)
}
