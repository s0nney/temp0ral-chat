package models

import "time"

type Message struct {
	ID        int
	Username  string
	Content   string
	UserID    string
	ImagePath string
	CreatedAt time.Time
}

type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}
