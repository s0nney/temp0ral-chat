package main

import (
	"log"

	"temp0ral-chat/controllers"
	"temp0ral-chat/models"
	"temp0ral-chat/routes"
	"temp0ral-chat/utils"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func main() {
	if err := utils.SetupDatabase(); err != nil {
		log.Fatalf("Failed to setup database: %v", err)
	}
	defer func() {
		if err := utils.CloseDatabase(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()

	controllers.StartPeriodicCleanup()
	log.Printf("Started periodic session and message cleanup with idle threshold: %v", models.IdleThreshold)

	go controllers.GlobalHub.RunSocket()

	r := gin.Default()
	routes.Temp0ralRouter(r)

	log.Printf("Server starting on :8080 with session duration: %v", models.SessionDuration)
	r.Run(":8080")
}
