package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fsdriver/internal/engine"
	"sync"
)

type Session struct {
    ID string
    Directories []string
    Engine *engine.WatcherEngine
    OutputChan chan engine.LogEvent
    Ctx context.Context
    Cancel context.CancelFunc
}

type SessionManager struct {
    mu sync.RWMutex
    sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
    return &SessionManager {
        sessions: make(map[string]*Session),
    }
}

func (sm *SessionManager) CreateSession(directories []string, initialFilters engine.FilterRules) (*Session, error) {
    sessionId, err := generateSessionId()
    if err != nil {
        return nil, err
    }

    ctx, cancel := context.WithCancel(context.Background())
    outputChan := make(chan engine.LogEvent, 1000) // Buffered channel to prevent blocking

    watcherEngine := engine.NewWatcherEngine(directories, initialFilters, outputChan)
    session := &Session {
        ID: sessionId,
        Directories: directories,
        Engine: watcherEngine,
        OutputChan: outputChan,
        Ctx: ctx,
        Cancel: cancel,
    }

    // start a go-routine
    go func() {
        _ = watcherEngine.Start(ctx)
    }()

    sm.mu.Lock()
    sm.sessions[sessionId] = session
    sm.mu.Unlock()

    return session, nil
}

func (sm *SessionManager) GetSession(id string) (*Session, bool) {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    session, exists := sm.sessions[id]
    return session, exists
}

func (sm *SessionManager) RemoveSession(id string) bool {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    session, exists := sm.sessions[id]
    if !exists {
        return false
    }

    session.Cancel()
    close(session.OutputChan)

    delete(sm.sessions, id)
    return true
}

func generateSessionId() (string, error) {
    bytes := make([]byte, 16)
    if _, err := rand.Read(bytes); err != nil {
        return "", err
    }

    return hex.EncodeToString(bytes), nil
}
