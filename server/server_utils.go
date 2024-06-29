package server

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sync"
)

type Action uint32

const (
	Register Action = iota
	Connect
	Login
	Tunnel
	JoinPool
)

type TokenStore struct {
	Tokens map[string]interface{}
	Mu     sync.Mutex
}

func (t *TokenStore) AddToken(key string, value interface{}) {
	t.Mu.Lock()
	defer t.Mu.Unlock()
	t.Tokens[key] = value
}

func (t *TokenStore) GetToken(key string) interface{} {
	t.Mu.Lock()
	defer t.Mu.Unlock()
	return t.Tokens[key]
}

func NewTokenStore() *TokenStore {
	// TODO : remove fixed value
	// TODO : persist tokens to db
	return &TokenStore{Tokens: map[string]interface{}{
		"LN97ccrfGrZX4rtiATmdDKImbQnbMW8BYWBWVrnfQpw=": "--",
	}}
}

func GetServerTlsConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair("serverCert.pem", "serverKey.pem")
	if err != nil {
		return nil, err
	}

	tlsConfig := tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	return &tlsConfig, nil
}

func CreateTlsServer(port int, handler func(*net.Conn)) (*Server, error) {
	tlsConfig, err := GetServerTlsConfig()
	if err != nil {
		return nil, err
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), tlsConfig)
	server := Server{
		IsTls:    true,
		Listener: listener,
		Done:     make(chan struct{}),
		Handler:  handler}

	return &server, nil
}

func CreatePlainServer(port int, handler func(*net.Conn)) (*Server, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	return &Server{IsTls: false,
		Listener: listener,
		Done:     make(chan struct{}),
		Handler:  handler}, nil
}

func GenerateToken(length uint) (string, error) {
	tokenBytes := make([]byte, length)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(tokenBytes), nil
}

func ReadSNIFromClientHello(peakReader *bufio.Reader) (string, error) {
	header, err := peakReader.Peek(9)
	if err != nil {
		return "", err
	}

	if len(header) < 9 {
		return "", errors.New("Data must be atleast 9 bytes")
	}

	if header[0] != 0x16 {
		return "", errors.New("Not a tls record header")
	}

	if header[1] != 0x03 || header[2] != 0x01 {
		return "", errors.New("Not tls 1.0")
	}

	recordLength := int(header[3])<<8 | int(header[4])
	fullRecord, err := peakReader.Peek(5 + recordLength)
	if err != nil {
		return "", err
	}

	if recordLength+5 != len(fullRecord) {
		return "", errors.New("Failed to fetch entire client hello")
	}

	// handshake header
	fullRecord = fullRecord[5:]
	if fullRecord[0] != 0x01 {
		return "", errors.New("Not client hello")
	}

	// delete handshake + length
	fullRecord = fullRecord[4:]

	// delete client version
	fullRecord = fullRecord[2:]

	// delete client random
	fullRecord = fullRecord[32:]

	sessionIDLength := int(fullRecord[0])

	// delete session id length
	fullRecord = fullRecord[1:]

	// delete session id
	fullRecord = fullRecord[sessionIDLength:]

	cipherSuiteLength := int(fullRecord[0])<<8 | int(fullRecord[1])

	// delete cipher suite length
	fullRecord = fullRecord[2:]

	// delete cipher suite
	fullRecord = fullRecord[cipherSuiteLength:]

	cmLength := int(fullRecord[0])

	// delete compression methods length
	fullRecord = fullRecord[1:]

	// delete cm
	fullRecord = fullRecord[cmLength:]

	// delete extension length
	fullRecord = fullRecord[2:]
	serverName := ""

	for {
		extensionData := int(fullRecord[0])<<8 | int(fullRecord[1])
		extensionLength := int(fullRecord[2])<<8 | int(fullRecord[3])

		if extensionData == 0 {
			// delete ex data and ex length
			fullRecord = fullRecord[4:]

			// delete all except extension
			fullRecord = fullRecord[:extensionLength]

			for {
				currLength := int(fullRecord[0])<<8 | int(fullRecord[1])
				if fullRecord[2] == 0x00 {
					// hostnameLength := int(fullRecord[3])<<8 | int(fullRecord[4])
					serverName = string(fullRecord[5:])
				}

				fullRecord = fullRecord[2+currLength:]
				break
			}

			break
		}

		fullRecord = fullRecord[4+extensionLength:]
	}

	return serverName, nil
}

func PrintHex(data []byte) {
	fmt.Print("Hex: [")

	for i, b := range data {
		if i > 0 {
			fmt.Print(" ")
		}

		fmt.Printf("%02x", b)
	}

	fmt.Println("]")
}
