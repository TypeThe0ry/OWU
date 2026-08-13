package gateway

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var errInvalidToken = errors.New("token is invalid, expired, or already used")

type memoryStore struct {
	mu       sync.Mutex
	secret   []byte
	tickets  map[[32]byte]launchState
	sessions map[[32]byte]sessionState
}

func newMemoryStore(secret []byte) *memoryStore {
	return &memoryStore{secret: append([]byte(nil), secret...), tickets: map[[32]byte]launchState{}, sessions: map[[32]byte]sessionState{}}
}

func (s *memoryStore) createTicket(state launchState) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(time.Now())
	s.tickets[s.digest(token)] = state
	return token, nil
}

func (s *memoryStore) consumeTicket(token string, now time.Time) (launchState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.digest(token)
	state, ok := s.tickets[key]
	delete(s.tickets, key)
	if !ok || !now.Before(state.ExpiresAt) {
		return launchState{}, errInvalidToken
	}
	return state, nil
}

func (s *memoryStore) createSession(state sessionState) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(time.Now())
	s.sessions[s.digest(token)] = state
	return token, nil
}

func (s *memoryStore) session(token string, now time.Time) (sessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.sessions[s.digest(token)]
	if !ok || !now.Before(state.ExpiresAt) {
		return sessionState{}, errInvalidToken
	}
	return state, nil
}

func (s *memoryStore) prune(now time.Time) {
	for key, state := range s.tickets {
		if !now.Before(state.ExpiresAt) {
			delete(s.tickets, key)
		}
	}
	for key, state := range s.sessions {
		if !now.Before(state.ExpiresAt) {
			delete(s.sessions, key)
		}
	}
}

func (s *memoryStore) digest(token string) [32]byte {
	h := hmac.New(sha256.New, s.secret)
	_, _ = h.Write([]byte(token))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
