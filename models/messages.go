package models

import "time"

type Message struct {
	ID        int
	Username  string
	Content   string
	CreatedAt time.Time
}
