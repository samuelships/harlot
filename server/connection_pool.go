package server

import (
	"errors"
	"net"
	"sync"
	"time"
)

const (
	MAX_CHAN_SIZE = 99999
)

var (
	PoolFullError        = errors.New("Pool is full")
	PoolEmptyError       = errors.New("Pool is empty")
	SessionNotFoundError = errors.New("Session not found")
	SubdomainNotFoundError = errors.New("Subdomain not found")
	SubdomainAlreadyExistsError = errors.New("Subdomain already exists")
)

type Conn struct {
	SessionID string
	Conn      *net.Conn
	StartTime time.Time
	Done chan struct{}
}

type Session struct {
	SessionID   string
	Subdomain   string
	TunnelConn  *net.Conn
	Connections chan *Conn
	ConnMu      sync.Mutex
	NextOpen    int
	done        chan struct{}
}

type ConnectionPooler struct {
	Sessions           map[string]*Session
	SubdomainToSession map[string]*Session
	SessMu             sync.Mutex
	IdleTimeout        time.Duration
}

func (cp *ConnectionPooler) IsSessionInPool(sessionID string) bool {
	cp.SessMu.Lock()
	defer cp.SessMu.Unlock()
	_, ok := cp.Sessions[sessionID]
	return ok
}

func (cp *ConnectionPooler) IsSubdomainInPool(subdomain string) bool {
	cp.SessMu.Lock()
	defer cp.SessMu.Unlock()
	_, ok := cp.SubdomainToSession[subdomain]
	return ok
}

func (cp *ConnectionPooler) Start() {
	go func() {
		cp.StartPrunner()
	}()
}

func (cp *ConnectionPooler) GetConn(sessionID string) (*Conn, error) {
	if !cp.IsSessionInPool(sessionID) {
		return nil, SessionNotFoundError
	}

	cp.SessMu.Lock()
	defer cp.SessMu.Unlock()
	sess := cp.Sessions[sessionID]

	select {
	case c := <-sess.Connections:
		return c, nil
	default:
		return nil, PoolEmptyError
	}
}

func (cp *ConnectionPooler) GetSession(subdomain string) (*Session, error) {
	if hasSub := cp.HasSubdomain(subdomain); !hasSub {
		return nil, SubdomainNotFoundError
	}

	cp.SessMu.Lock()
	defer cp.SessMu.Unlock()
	return cp.SubdomainToSession[subdomain], nil
}

func (cp *ConnectionPooler) PutConn(sessionID string, c *Conn) error {
	if !cp.IsSessionInPool(sessionID) {
		return SessionNotFoundError
	}

	sess := cp.Sessions[sessionID]
	sess.ConnMu.Lock()
	defer sess.ConnMu.Unlock()

	select {
	case sess.Connections <- c:
	default:
		return PoolFullError
	}

	return nil
}

func (cp *ConnectionPooler) StartPrunner() {
	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			cp.Prune()
		}
	}
}

func (cp *ConnectionPooler) Prune() {
	for _, sess := range cp.Sessions {
		sess.ConnMu.Lock()
		connLength := len(sess.Connections)

		for i := 0; i < connLength; i++ {
			curr := <-sess.Connections
			now := time.Now()

			if now.Sub(curr.StartTime) < cp.IdleTimeout {
				sess.Connections <- curr
			} else {
				(*curr.Conn).Close()
			}
		}

		sess.ConnMu.Unlock()
		newConnLength := len(sess.Connections)
		nextOpen := GetNextOpen(newConnLength)
		sess.NextOpen = nextOpen
	}
}

func (cp *ConnectionPooler) AddSession(sessionID, subdomain string, tunnel *net.Conn) (*Session, error) {
	cp.SessMu.Lock()
	defer cp.SessMu.Unlock()

	if _, alreadyIn := cp.SubdomainToSession[subdomain]; alreadyIn {
		return nil, SubdomainAlreadyExistsError
	}

	newSession := &Session{
		SessionID:  sessionID,
		Subdomain:  subdomain,
		TunnelConn: tunnel, NextOpen: 5,
		Connections: make(chan *Conn, MAX_CHAN_SIZE),
	}

	cp.SubdomainToSession[subdomain] = newSession
	cp.Sessions[sessionID] = newSession
	return newSession, nil
}

func (cp *ConnectionPooler) RemoveSession(sessionID string) error {
	if !cp.IsSessionInPool(sessionID) {
		return SessionNotFoundError
	}

	cp.SessMu.Lock()
	defer cp.SessMu.Unlock()

	session := cp.Sessions[sessionID]
	subdomain := (*session).Subdomain

	delete(cp.Sessions, sessionID)
	delete(cp.SubdomainToSession, subdomain)
	return nil
}

func (cp *ConnectionPooler) HasSession(sessionID string) bool {
	return cp.IsSessionInPool(sessionID)
}

func (cp *ConnectionPooler) HasSubdomain(subdomain string) bool {
	return cp.IsSubdomainInPool(subdomain)
}

func (cp *ConnectionPooler) OpenMoreConns(session *Session) error {
	tunnelConn := (*session.TunnelConn)
	err := WriteUint32(tunnelConn, uint32(session.NextOpen))
	session.NextOpen = GetNextOpen(session.NextOpen)
	return err
}

func GetNextOpen(length int) int {
	newLimit := 5
	for start := 5; start != 0; start *= 2 {
		if start > length {
			newLimit = start
			break
		}
	}
	return newLimit
}

func NewConnectionPooler() *ConnectionPooler {
	return &ConnectionPooler{
		Sessions:           map[string]*Session{},
		SubdomainToSession: map[string]*Session{},
		IdleTimeout:        1 * time.Minute,
	}
}
