package client

import (
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/samuelships/harlot/server"
	"github.com/samuelships/harlot/utils"
)

var MainSessionStore = NewSessionStore()
var MainReqResQueue = NewReqResQueue()

type Client struct {
	Conn    *net.Conn
	Address string
}

func NewClient(address string) (*Client, error) {
	conn, err := dialTls(address)
	return &Client{Conn: &conn, Address: address}, err
}

func (c *Client) FromOld() (*Client, error) {
	conn, err := dialTls(c.Address)
	return &Client{Conn: &conn, Address: c.Address}, err
}

func (c *Client) Register() (string, error) {
	err := server.WriteUint32(*c.Conn, uint32(server.Register))
	if err != nil {
		return "", utils.LogErrorReturn("Failed to write register action %w", err)
	}

	utils.LogInfo("Successfully wrote register action")

	length, err := server.ReadUint32(*c.Conn)
	if err != nil {
		return "", utils.LogErrorReturn("Failed to read length of token %w", err)
	}

	token, err := server.ReadIntoBuffer(*c.Conn, length)
	if err != nil {
		return "", utils.LogErrorReturn("Failed to read token %v", err)
	}

	utils.LogInfo("Token received", slog.String("token", string(token)))
	return string(token), nil
}

func (c *Client) Login(serverUrl, token string) (bool, error) {
	err := server.WriteUint32(*c.Conn, uint32(server.Login))
	if err != nil {
		return false, utils.LogErrorReturn("Failed to write login action : %w", err)
	}

	tokenLen := uint32(len(token))
	err = server.WriteUint32(*c.Conn, tokenLen)
	if err != nil {
		return false, utils.LogErrorReturn("Failed to write token length : %w", err)
	}

	server.WriteBuffer(*c.Conn, []byte(token))
	if err != nil {
		return false, utils.LogErrorReturn("Failed to write token : %w", err)
	}

	isOk, err := server.ReadBool(*c.Conn)
	if err != nil {
		return false, utils.LogErrorReturn("Failed to read result : %w", err)
	}

	return isOk, nil
}

func logTunnelSuccess(protocol, subdomain, serverUrl string) {
	serverUrl = strings.Split(serverUrl, ":")[0]
	tunnelUrl := fmt.Sprintf("https://%s", subdomain + "." + serverUrl)
	utils.LogInfo(fmt.Sprintf("Tunnel established! Access your service at %s", tunnelUrl))
}

func logTunnelError() {
	utils.LogInfo("Error establishing tunnel")
}

func (c *Client) Tunnel(serverUrl, token, subdomain, protocol string, isTls bool, port int) error {
	defer (*c.Conn).Close()

	// action
	err := server.WriteUint32(*c.Conn, uint32(server.Tunnel))
	if err != nil {
		return utils.LogErrorReturn("Failed to write tunnel action : %w", err)
	}

	// token length
	err = server.WriteUint32(*c.Conn, uint32(len(token)))
	if err != nil {
		return utils.LogErrorReturn("Failed to write token length : %w", err)
	}

	// token
	err = server.WriteBuffer(*c.Conn, []byte(token))
	if err != nil {
		return utils.LogErrorReturn("Failed to write token : %w", err)
	}

	sessionID, err := server.GenerateToken(32)

	// session id length
	err = server.WriteUint32(*c.Conn, uint32(len(sessionID)))
	if err != nil {
		return utils.LogErrorReturn("Failed to write tunnel action : %w", err)
	}

	// session
	err = server.WriteBuffer(*c.Conn, []byte(sessionID))
	if err != nil {
		return utils.LogErrorReturn("Failed to write token : %w", err)
	}

	// subdomain length
	err = server.WriteUint32(*c.Conn, uint32(len(subdomain)))
	if err != nil {
		return utils.LogErrorReturn("Failed to write subdomain length : %w", err)
	}

	// subdomain
	err = server.WriteBuffer(*c.Conn, []byte(subdomain))
	if err != nil {
		return utils.LogErrorReturn("Failed to write subdomain : %w", err)
	}

	// read status (sucess / error)
	success, err := server.ReadBool(*c.Conn)
	if err != nil {
		logTunnelError()
		return utils.LogErrorReturn("Failed to read success message : %v", err)
	}

	if !success {
		logTunnelError()
		return utils.LogErrorReturn("Error in creating session : %v", err)
	}

	logTunnelSuccess(protocol, subdomain, serverUrl)

	// add service
	service := &Service{IsTls: isTls, Port: port, Protocol: protocol}
	MainSessionStore.AddService(sessionID, service)
	defer MainSessionStore.RemoveService(sessionID)

	for {
		spawnCount, err := server.ReadUint32(*c.Conn)
		if err != nil {
			return utils.LogErrorReturn("Failed to read spawn Value : %v", err)
		}

		SpinUp(c, sessionID, spawnCount)
	}
}

func (c *Client) PoolWorker(sessionID string) error {
	defer (*c.Conn).Close()

	// action
	err := server.WriteUint32(*c.Conn, uint32(server.JoinPool))
	if err != nil {
		return utils.LogErrorReturn("Failed to write action : %w", err)
	}

	// write session id length
	err = server.WriteUint32(*c.Conn, uint32(len(sessionID)))
	if err != nil {
		return utils.LogErrorReturn("Failed to write session id length : %w", err)
	}

	// write session id
	err = server.WriteBuffer(*c.Conn, []byte(sessionID))
	if err != nil {
		return utils.LogErrorReturn("Failed to write session id : %w", err)
	}

	// read success
	success, err := server.ReadBool(*c.Conn)
	if err != nil {
		return utils.LogErrorReturn("Failed to read success : %w", err)
	}

	if !success {
		return utils.LogErrorReturn("Error joining pool : %w", err)
	}

	ProxyToLocal(sessionID, c.Conn)
	return nil
}

func SpinUp(client *Client, sessionID string, spawnCount uint32) {
	for i := 0; i < int(spawnCount); i++ {
		go func() {
			newClient, err := (*client).FromOld()
			if err != nil {
				utils.LogInfo("Error creating client")
			}

			err = newClient.PoolWorker(sessionID)
			if err != nil {
				utils.LogInfo("Error joining pool")
			}
		}()
	}
}
