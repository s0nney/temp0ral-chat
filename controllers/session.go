package controllers

import (
	"temp0ral-chat/helpers"
	"temp0ral-chat/models"
	"time"
)

func CreateSession() models.Session {
	sessionID := helpers.GenerateID(16)
	userID := helpers.GenerateID(8)

	session := models.Session{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(models.SessionDuration),
		CreatedAt: time.Now(),
	}

	models.SessionsMutex.Lock()
	models.Sessions[sessionID] = session
	models.SessionsMutex.Unlock()

	helpers.UpdateUserActivity(userID)

	return session
}

func GetSession(sessionID string) (models.Session, bool) {
	models.SessionsMutex.RLock()
	defer models.SessionsMutex.RUnlock()

	session, exists := models.Sessions[sessionID]
	if !exists {
		return models.Session{}, false
	}

	if time.Now().After(session.ExpiresAt) {
		return models.Session{}, false
	}

	return session, true
}
