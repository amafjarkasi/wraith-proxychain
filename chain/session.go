package chain

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/wraith/proxychain/hop"
)

type session struct {
	Order   []string  `json:"order"`
	Expires time.Time `json:"expires"`
}

// SessionStore allows pluggable session persistence (file, Redis, etc.).
type SessionStore interface {
	Get(key string) ([]*hop.Proxy, bool)
	Set(key string, order []*hop.Proxy, ttl time.Duration)
}

type sessionStore struct {
	mu   sync.Mutex
	ttl  time.Duration
	file string
	m    map[string]*session
}

func newSessionStore(ttl time.Duration, file string) *sessionStore {
	s := &sessionStore{ttl: ttl, file: file, m: make(map[string]*session)}
	s.load()
	return s
}

func (s *sessionStore) load() {
	if s.file == "" {
		return
	}
	b, err := os.ReadFile(s.file)
	if err != nil {
		return
	}
	var data map[string]*session
	if err := json.Unmarshal(b, &data); err != nil {
		return
	}
	now := time.Now()
	for k, v := range data {
		if now.Before(v.Expires) {
			s.m[k] = v
		}
	}
}

func (s *sessionStore) persist() {
	if s.file == "" {
		return
	}
	data := make(map[string]*session)
	for k, v := range s.m {
		data[k] = v
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	os.WriteFile(s.file, b, 0644)
}

func (s *sessionStore) Get(key string) ([]*hop.Proxy, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.Expires) {
			delete(s.m, k)
		}
	}
	v, ok := s.m[key]
	if !ok || now.After(v.Expires) {
		if ok {
			delete(s.m, key)
		}
		return nil, false
	}
	out := make([]*hop.Proxy, 0, len(v.Order))
	for _, u := range v.Order {
		p, err := hop.New(u)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func (s *sessionStore) Set(key string, order []*hop.Proxy, ttl time.Duration) {
	if ttl <= 0 {
		ttl = s.ttl
	}
	urls := make([]string, 0, len(order))
	for _, p := range order {
		if p != nil && p.URL != nil {
			urls = append(urls, p.URL.String())
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = &session{Order: urls, Expires: time.Now().Add(ttl)}
	s.persist()
}
