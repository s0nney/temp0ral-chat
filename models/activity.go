package models

import (
	"sync"
	"time"
)

var UserLastActivity = make(map[string]time.Time)
var ActivityMutex sync.RWMutex
