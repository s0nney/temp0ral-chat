package models

import "time"

const (
	DBUser          = ""
	DBPassword      = ""
	DBName          = ""
	DBHost          = "localhost"
	DBPort          = "5432"
	AccessKey       = "test" // change in prod!
	SessionDuration = 5 * time.Hour
	CleanupInterval = 30 * time.Second
	MaxUploadSize   = 5 * 1024 * 1024  // 5 MB upload limit!
	IdleThreshold   = 3 * time.Second  // Users idle after N seconds of no activity
	MaxIdleTime     = 60 * time.Second // Terminate sessions after N minutes of inactivity
)
