package models

import "sync"

var Sessions = make(map[string]Session)
var SessionsMutex sync.RWMutex
