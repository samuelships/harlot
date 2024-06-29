package server

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/samuelships/harlot/utils"
)

func ReadUint32(reader io.Reader) (uint32, error) {
	var result uint32
	err := binary.Read(reader, binary.BigEndian, &result)
	return result, err
}

func ReadBool(reader io.Reader) (bool, error) {
	var result bool
	err := binary.Read(reader, binary.BigEndian, &result)
	return result, err
}

func ReadIntoBuffer(reader io.Reader, bLen uint32) ([]byte, error) {
	buffer := make([]byte, bLen)
	err := binary.Read(reader, binary.BigEndian, buffer)
	return buffer, err
}

func WriteBool(writer io.Writer, value bool) error {
	err := binary.Write(writer, binary.BigEndian, value)
	return err
}

func WriteUint32(writer io.Writer, value uint32) error {
	err := binary.Write(writer, binary.BigEndian, value)
	return err
}

func WriteBuffer(writer io.Writer, value []byte) error {
	err := binary.Write(writer, binary.BigEndian, value)
	return err
}

func HandleLoginAction(conn *net.Conn) {
	tokenLength, err := ReadUint32(*conn)
	if err != nil {
		utils.LogInfo("Failed to read token length", err)
		return
	}

	tokenBuf, err := ReadIntoBuffer(*conn, tokenLength)
	if err != nil {
		utils.LogInfo("Failed to read token", err)
		return
	}

	isTokenValid := false
	result := MainTokenStore.GetToken(string(tokenBuf))
	if result != nil {
		isTokenValid = true
	}

	err = WriteBool(*conn, isTokenValid)
	if err != nil {
		utils.LogInfo("Failed to write result", err)
		return
	}
}

func HandleRegisterAction(conn *net.Conn) {
	token, err := GenerateToken(32)
	MainTokenStore.AddToken(token, "")
	if err != nil {
		utils.LogInfo("error generating token", err)
		return
	}

	err = WriteUint32(*conn, uint32(len(token)))
	if err != nil {
		utils.LogInfo("error writing length of register token", err)
		return
	}

	err = WriteBuffer(*conn, []byte(token))
	if err != nil {
		utils.LogInfo("error writing register token", err)
		return
	}
}

func HandleTunnelServer(conn *net.Conn) {
	// token length
	tokenLength, err := ReadUint32(*conn)
	if err != nil {
		utils.LogInfo("Failed to read token length", err)
		return
	}

	// token
	tokenBuf, err := ReadIntoBuffer(*conn, tokenLength)
	if err != nil {
		utils.LogInfo("Failed to read token", err)
		return
	}

	// validate token
	isTokenValid := false
	result := MainTokenStore.GetToken(string(tokenBuf))
	if result != nil {
		isTokenValid = true
	}

	if !isTokenValid {
		utils.LogInfo("Token is invalid", err)
		return
	}

	// session id
	sessionIDLength, err := ReadUint32(*conn)
	if err != nil {
		utils.LogInfo("Failed to read session id length", err)
		return
	}

	// session string
	sessionBuf, err := ReadIntoBuffer(*conn, sessionIDLength)
	if err != nil {
		utils.LogInfo("Failed to read token", err)
		return
	}
	sessionStr := string(sessionBuf)

	// subdomain length
	subdomainLength, err := ReadUint32(*conn)
	if err != nil {
		utils.LogInfo("Failed to read subdomain length", err)
		return
	}

	// subdomain
	subdomainBuf, err := ReadIntoBuffer(*conn, subdomainLength)
	if err != nil {
		utils.LogInfo("Failed to read token", err)
		return
	}
	subdomainStr := string(subdomainBuf)

	_, err = MainConnectionPooler.AddSession(sessionStr, subdomainStr, conn)
	defer MainConnectionPooler.RemoveSession(sessionStr)

	if err != nil {
		utils.LogInfo("Failed to start session", err)
		return
	}

	// write success
	err = WriteBool(*conn, true)
	if err != nil {
		utils.LogInfo("Failed to write success message", err)
		return
	}

	for {
		// detect when connection dies
		_, err := ReadIntoBuffer(*conn, 1)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// connection is closed
			}

			break
		}
	}
}

func HandleJoinPool(conn *net.Conn) {
	// session id length
	sessionIDLength, err := ReadUint32(*conn)
	if err != nil {
		utils.LogInfo("Failed to read session id length", err)
		return
	}

	// session id
	sessionIDBuf, err := ReadIntoBuffer(*conn, sessionIDLength)
	if err != nil {
		utils.LogInfo("Failed to read token", err)
		return
	}

	sessionIDStr := string(sessionIDBuf)

	// verify session id
	hasSession := MainConnectionPooler.HasSession(sessionIDStr)

	success := false
	if hasSession {
		success = true
	}

	err = WriteBool(*conn, success)
	if err != nil {
		utils.LogInfo("Failed varto write success message", err)
		return
	}

	done := make(chan struct{})
	wrappedConn := &Conn{
		SessionID: sessionIDStr,
		Conn:      conn,
		StartTime: time.Now(),
		Done:      done,
	}

	err = MainConnectionPooler.PutConn(sessionIDStr, wrappedConn)
	if err != nil {
		utils.LogInfo("Failed to add session to pool", err)
		return
	}

	<-wrappedConn.Done
}
