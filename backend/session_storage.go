package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go-diploma-issuer/diploma"

	"github.com/redis/go-redis/v9"
)

// SessionStage tracks how far an upload session has progressed.
type SessionStage string

const (
	// StageValidated: the extracts have been verified and parsed; waiting for
	// the identity disclosure to start.
	StageValidated SessionStage = "validated"
	// StageDisclosing: an IRMA disclosure session has been started; waiting
	// for its result.
	StageDisclosing SessionStage = "disclosing"
)

// SessionLifetime bounds how long uploaded extracts are kept server side.
const SessionLifetime = time.Hour

// Session is the server side state of one issuance flow: one or more verified
// diploma extracts of the same holder.
type Session struct {
	Id        string       `json:"id"`
	CreatedAt time.Time    `json:"created_at"`
	Stage     SessionStage `json:"stage"`
	// The verified and parsed extracts, in upload order. All name the same
	// holder.
	Documents []*diploma.Document `json:"documents"`
	// Requestor token of the IRMA disclosure session, set in StageDisclosing.
	IrmaToken string `json:"irma_token,omitempty"`
}

// SessionStorage persists sessions between the upload, disclosure and issuance
// requests. Implementations must be safe for concurrent use.
type SessionStorage interface {
	// Store saves or replaces the session.
	Store(session *Session) error
	// Retrieve returns the session or an error when it does not exist (or has
	// expired).
	Retrieve(sessionId string) (*Session, error)
	// Remove deletes the session; a missing session is an error.
	Remove(sessionId string) error
}

// ------------------------------------------------------------------------------

// InMemorySessionStorage keeps sessions in a map. Suitable for a single
// instance deployment and for tests.
type InMemorySessionStorage struct {
	sessions map[string]*Session
	mutex    sync.Mutex
	now      func() time.Time
}

func NewInMemorySessionStorage() *InMemorySessionStorage {
	return &InMemorySessionStorage{
		sessions: make(map[string]*Session),
		now:      time.Now,
	}
}

func (s *InMemorySessionStorage) Store(session *Session) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.expireLocked()
	copied := *session
	s.sessions[session.Id] = &copied
	return nil
}

func (s *InMemorySessionStorage) Retrieve(sessionId string) (*Session, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.expireLocked()
	session, ok := s.sessions[sessionId]
	if !ok {
		return nil, fmt.Errorf("no session found for %s", sessionId)
	}
	copied := *session
	return &copied, nil
}

func (s *InMemorySessionStorage) Remove(sessionId string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.sessions[sessionId]; !ok {
		return fmt.Errorf("failed to remove session %s, because it wasn't there", sessionId)
	}
	delete(s.sessions, sessionId)
	return nil
}

// expireLocked drops sessions older than SessionLifetime. Caller holds the lock.
func (s *InMemorySessionStorage) expireLocked() {
	cutoff := s.now().Add(-SessionLifetime)
	for id, session := range s.sessions {
		if session.CreatedAt.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}

// ------------------------------------------------------------------------------

// RedisSessionStorage stores sessions as JSON in Redis with a TTL.
type RedisSessionStorage struct {
	client    *redis.Client
	namespace string
}

func NewRedisSessionStorage(client *redis.Client, namespace string) *RedisSessionStorage {
	return &RedisSessionStorage{client: client, namespace: namespace}
}

func (s *RedisSessionStorage) key(sessionId string) string {
	return fmt.Sprintf("%s:session:%s", s.namespace, sessionId)
}

func (s *RedisSessionStorage) Store(session *Session) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}
	return s.client.Set(context.Background(), s.key(session.Id), payload, SessionLifetime).Err()
}

func (s *RedisSessionStorage) Retrieve(sessionId string) (*Session, error) {
	payload, err := s.client.Get(context.Background(), s.key(sessionId)).Bytes()
	if err != nil {
		return nil, err
	}
	var session Session
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}
	return &session, nil
}

func (s *RedisSessionStorage) Remove(sessionId string) error {
	removed, err := s.client.Del(context.Background(), s.key(sessionId)).Result()
	if err != nil {
		return err
	}
	if removed == 0 {
		return fmt.Errorf("failed to remove session %s, because it wasn't there", sessionId)
	}
	return nil
}
