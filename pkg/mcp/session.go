package mcp

import (
	"sync"
	"time"
)

// Session wraps a client instance and its lifecycle metadata.
type Session struct {
	Client   *Client
	lastUsed time.Time
}

// SessionCache keeps MCP client sessions keyed by server identifier.
type SessionCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	items map[string]*Session
}

// NewSessionCache creates a cache with the provided TTL. Zero TTL disables expiry.
func NewSessionCache(ttl time.Duration) *SessionCache {
	return &SessionCache{
		ttl:   ttl,
		now:   time.Now,
		items: make(map[string]*Session),
	}
}

// Get returns a cached client or builds a new one using builder when missing/expired.
// The boolean indicates whether an existing session was reused.
func (c *SessionCache) Get(key string, builder func() (*Client, error)) (*Client, bool, error) {
	if builder == nil {
		return nil, false, ErrTransportClosed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if session, ok := c.items[key]; ok && !c.expired(session) {
		session.lastUsed = c.now()
		return session.Client, true, nil
	}

	newClient, err := builder()
	if err != nil {
		return nil, false, err
	}
	c.items[key] = &Session{
		Client:   newClient,
		lastUsed: c.now(),
	}
	return newClient, false, nil
}

// CloseIdle forces expiration and Close on stale sessions.
func (c *SessionCache) CloseIdle() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, session := range c.items {
		if session == nil || !c.expired(session) {
			continue
		}
		if session.Client != nil {
			_ = session.Client.Close()
		}
		delete(c.items, key)
	}
	return nil
}

// CloseAll tears down every cached session.
func (c *SessionCache) CloseAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, session := range c.items {
		if session != nil && session.Client != nil {
			_ = session.Client.Close()
		}
		delete(c.items, key)
	}
	return nil
}

func (c *SessionCache) expired(s *Session) bool {
	if c.ttl <= 0 || s == nil {
		return false
	}
	return s.lastUsed.Add(c.ttl).Before(c.now())
}
