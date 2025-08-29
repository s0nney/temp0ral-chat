package utils

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"temp0ral-chat/models"
)

var DB *sql.DB

func SetupDatabase() error {
	var err error

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		models.DBHost,
		models.DBPort,
		models.DBUser,
		models.DBPassword,
		models.DBName,
	)

	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("DB connection error: %w", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(5)

	log.Println("Database connection established successfully")

	if err := createTables(); err != nil {
		return err
	}

	if err := createUploadsDirectory(); err != nil {
		return err
	}

	return nil
}

func createTables() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id SERIAL PRIMARY KEY,
			username VARCHAR(255) NOT NULL,
			content TEXT NOT NULL,
			user_id VARCHAR(255) NOT NULL,
			image_path VARCHAR(255),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("table creation error: %w", err)
	}

	_, err = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id)
	`)
	if err != nil {
		return fmt.Errorf("user_id index creation error: %w", err)
	}

	_, err = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at DESC)
	`)
	if err != nil {
		return fmt.Errorf("created_at index creation error: %w", err)
	}

	log.Println("Database tables and indexes created successfully")
	return nil
}

func createUploadsDirectory() error {
	err := os.MkdirAll("./uploads", 0755)
	if err != nil {
		return fmt.Errorf("error creating uploads directory: %w", err)
	}
	log.Println("Uploads directory created successfully")
	return nil
}

func CloseDatabase() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
