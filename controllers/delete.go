package controllers

import (
	"database/sql"
	"log"
	"os"
	"temp0ral-chat/utils"
)

func DeleteImagesForQuery(query string, args ...interface{}) {
	rows, err := utils.DB.Query(query, args...)
	if err != nil {
		log.Printf("Error querying images to delete: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var imagePath sql.NullString
		if err := rows.Scan(&imagePath); err != nil {
			log.Printf("Error scanning image path: %v", err)
			continue
		}
		if imagePath.Valid && imagePath.String != "" {
			err := os.Remove("." + imagePath.String)
			if err != nil {
				log.Printf("Error deleting image file %s: %v", imagePath.String, err)
			} else {
				log.Printf("Deleted image file: %s", imagePath.String)
			}
		}
	}
}
