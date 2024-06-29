package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/samuelships/harlot/utils"
)

var MainConnectionPooler = NewConnectionPooler()
var MainTokenStore = NewTokenStore()

const (
	ConnectionGetWaitTimeoutSecs = 5
	ConnectionGetRetry           = 1
)

type Server struct {
	IsTls    bool
	Listener net.Listener
	Done     chan struct{}
	Handler  func(*net.Conn)
}

func (s *Server) Start() {
	defer s.Listener.Close()
	for {
		conn, err := s.Listener.Accept()
		if err != nil {
			utils.LogInfo("failed to accept connection %e", err)
			close(s.Done)
			return
		}

		go s.Handler(&conn)
	}
}

func PrivateServerHandler(conn *net.Conn) {
	defer (*conn).Close()
	for {
		var action Action
		actionUint, err := ReadUint32(*conn)
		action = Action(actionUint)
		if err != nil {
			utils.LogInfo("Failed to read action", err)
			return
		}

		switch action {
		case Register:
			HandleRegisterAction(conn)
			return
		case Login:
			HandleLoginAction(conn)
			return
		case Tunnel:
			HandleTunnelServer(conn)
			return
		case JoinPool:
			HandleJoinPool(conn)
			return
		default:
			utils.LogError("invalid action")
			return
		}
	}
}

func PublicServerHandler(conn *net.Conn) {
	defer (*conn).Close()

	peakConn := bufio.NewReader((*conn))
	sniName, err := ReadSNIFromClientHello(peakConn)
	if err != nil {
		utils.LogInfo("Could not read sni name from tls connection")
		return
	}

	sniNameSplit := strings.Split(sniName, ".")
	if len(sniNameSplit) < 1 {
		utils.LogInfo("We need atleast 1 parts")
		return
	}

	subdomain := sniNameSplit[0]
	if hasSubdomain := MainConnectionPooler.HasSubdomain(subdomain); !hasSubdomain {
		utils.LogInfo("Subdomain does not exist")
		return
	}

	session, err := MainConnectionPooler.GetSession(subdomain)
	if err != nil {
		utils.LogError("Subdomain does not exist : %w", err)
		return
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second*ConnectionGetWaitTimeoutSecs,
	)

	defer cancel()

	var poolConn *Conn
	var calledOpened = false
retry:
	select {
	case <-ctx.Done():
		utils.LogError("Error getting connection to proxy to : %w", err)
		return
	default:
		poolConn, err = MainConnectionPooler.GetConn(session.SessionID)
		if err != nil {
			if !errors.Is(err, PoolEmptyError) {
				utils.LogError("Error getting connection to proxy to : %v \n", err)
				return
			}

			if !calledOpened {
				MainConnectionPooler.OpenMoreConns(session)
				calledOpened = true
			}

			time.Sleep(ConnectionGetRetry * time.Millisecond)
			goto retry
		}
	}

	go func() {
		_, err := io.Copy((*poolConn.Conn), peakConn)
		if err != nil {
			// utils.LogDebug("error copying into session conn")
		}

		(*poolConn.Conn).Close()
		// utils.LogDebug(fmt.Sprintf("copied %d into session conn\n", n))
	}()

	_, err = io.Copy((*conn), (*poolConn.Conn))
	if err != nil {
		// utils.LogError("error copying into conn")
	}

	(*poolConn).Done <- struct{}{}
}
