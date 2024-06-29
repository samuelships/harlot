package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/samuelships/harlot/server"
	"github.com/samuelships/harlot/utils"
	"golang.org/x/exp/slices"
)

var (
	SessionStoreNotFoundErr = errors.New("Not Found")
)

type WrappedReq struct {
	Req *http.Request
}

type WrappedResp struct {
	Resp *http.Response
	Body []byte
}

type ReqResQueue struct {
	Requests  []*WrappedReq
	Responses []*WrappedResp
	Mu        sync.Mutex
	Logger    io.Writer
}

func (rrq *ReqResQueue) AddRequest(req *WrappedReq) error {
	rrq.Mu.Lock()
	defer rrq.Mu.Unlock()
	rrq.Requests = append(rrq.Requests, req)
	if len(rrq.Requests) > 0 && len(rrq.Responses) > 0 {
		rrq.LogResponse()
	}
	return nil
}

func (rrq *ReqResQueue) AddResponse(resp *WrappedResp) error {
	rrq.Mu.Lock()
	defer rrq.Mu.Unlock()
	rrq.Responses = append(rrq.Responses, resp)
	if len(rrq.Requests) > 0 && len(rrq.Responses) > 0 {
		rrq.LogResponse()
	}
	return nil
}

func (rrq *ReqResQueue) LogResponse() error {
	logRequestResponse(rrq.Requests[0], rrq.Responses[0])
	rrq.Responses = rrq.Responses[1:]
	rrq.Requests = rrq.Requests[1:]
	return nil
}

func NewReqResQueue() *ReqResQueue {
	return &ReqResQueue{
		Requests:  []*WrappedReq{},
		Responses: []*WrappedResp{},
		Mu:        sync.Mutex{},
		Logger:    os.Stdin,
	}
}

type Service struct {
	Protocol string // valid : http / https / tcp / tcps
	IsTls    bool
	Port     int
}

type SessionStore struct {
	Services map[string]*Service
	Mu       sync.Mutex
}

func (sess *SessionStore) AddService(sessionID string, service *Service) error {
	sess.Mu.Lock()
	defer sess.Mu.Unlock()
	sess.Services[sessionID] = service
	return nil
}

func (sess *SessionStore) Get(sessionID string) (*Service, error) {
	sess.Mu.Lock()
	defer sess.Mu.Unlock()
	if val, ok := sess.Services[sessionID]; ok {
		return val, nil
	}

	return nil, SessionStoreNotFoundErr
}

func (sess *SessionStore) RemoveService(sessionID string) error {
	sess.Mu.Lock()
	defer sess.Mu.Unlock()
	delete(sess.Services, sessionID)
	return nil
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		Mu:       sync.Mutex{},
		Services: map[string]*Service{},
	}
}

func GetConfigDir() (string, error) {
	if runtime.GOOS == "windows" {
		panic(utils.LogErrorReturn("Operating system not implemented"))
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", utils.LogErrorReturn("Failed to get home dir %w", err)
	}

	return filepath.Join(homedir, "./harlot"), nil
}

func GetTokenFromConfig() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}

	file, err := os.ReadFile(configDir + "/.config")
	if err != nil {
		return "", err
	}

	return string(file), nil
}

func PersistToken(token string) error {
	fullPath, err := GetConfigDir()
	if err != nil {
		return utils.LogErrorReturn("Failed to get config dir %v", err)
	}

	err = os.MkdirAll(fullPath, 755)
	if err != nil {
		if !os.IsExist(err) {
			return utils.LogErrorReturn("Failed to create dir %v", err)
		}
	}

	file, err := os.OpenFile(fullPath+"/.config", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 06444)
	defer file.Close()
	if err != nil {
		if !os.IsExist(err) {
			return utils.LogErrorReturn("Failed to create file %v", err)
		}
	}

	_, err = file.WriteString(token)
	if err != nil {
		return utils.LogErrorReturn("Failed to write file %v", err)
	}

	return nil
}

// conn is spun by opening a tcp connection to the remote server
// this function BLOCKS HERE <-- until the server matches with another incoming
// and starts sending data down to us
func ProxyToLocal(sessionID string, conn *net.Conn) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
	}()

	service, err := MainSessionStore.Get(sessionID)
	if err != nil {
		return err
	}

	var remote io.ReadWriteCloser = *conn

	// server gives us a regular tcp connection
	// except its tls - terminate it here
	tlsConfig, err := server.GetServerTlsConfig()
	if err != nil {
		return err
	}

	tlsConn := tls.Server(*conn, tlsConfig)
	// <- blocks below until server starts proxying data
	// using this connection
	err = tlsConn.Handshake() // <- blocks here
	remote = tlsConn
	if err != nil {
		return err
	}

	var local io.ReadWriteCloser
	servicePort := fmt.Sprintf(":%d", service.Port)

	if service.IsTls {
		local, err = tls.Dial(
			"tcp",
			servicePort,
			getTlsConfig(),
		)
	} else {
		local, err = net.Dial("tcp", servicePort)
	}

	if err != nil {
		utils.LogInfo("Error dialing local service : %w", err)
		return err
	}

	// for reading requests
	remoteReader, remoteSpyReader := createSpyReader(remote)
	localReader, localSpyReader := createSpyReader(local)

	httpProtocols := []string{"http", "https"}
	if slices.Contains(httpProtocols, service.Protocol) {
		go func() {
			readRequestsLoop(remoteSpyReader, ctx)
		}()

		go func() {
			readResponsesLoop(localSpyReader, ctx)
		}()
	}

	go func() {
		io.Copy(local, remoteReader)
		// utils.LogInfo("Finished copying into local")
		local.Close()
	}()

	_, err = io.Copy(remote, localReader)
	if err != nil {
		// utils.LogInfo("Error copying into remote")
	}

	remote.Close()
	// utils.LogInfo("Finished copying to remote")
	return nil
}

func createSpyReader(reader io.Reader) (io.Reader, io.Reader) {
	bufferStorage := bytes.Buffer{}
	spyReader := io.TeeReader(reader, &bufferStorage)
	return spyReader, &bufferStorage
}

// TODO : add support for HTTP/2
func readRequestsLoop(reader io.Reader, ctx context.Context) {
	prevRequestData := []byte{}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var requestReader io.Reader
		var requestDataRead bytes.Buffer

		if len(prevRequestData) > 0 {
			requestReader = io.MultiReader(bytes.NewBuffer(prevRequestData), reader)
			prevRequestData = []byte{}
		} else {
			requestReader = reader
		}

		bRequestReader := bufio.NewReader(requestReader)

		unreadLast := func() {
			totalRead := requestDataRead.Len()
			tempBuffer := make([]byte, totalRead)
			requestDataRead.Read(tempBuffer)
			prevRequestData = tempBuffer
		}

		req, err := http.ReadRequest(bRequestReader)
		if err != nil {
			unreadLast()
			continue
		}

		_, err = io.ReadAll(req.Body)
		if err != nil {
			unreadLast()
			continue
		}

		// we might still have some bytes in the buffer save for next round
		unreadBufferSize := bRequestReader.Buffered()
		if unreadBufferSize > 0 {
			prevRequestData = make([]byte, unreadBufferSize)
			bRequestReader.Read(prevRequestData)
		}

		MainReqResQueue.AddRequest(&WrappedReq{req})
	}
}

func readResponsesLoop(reader io.Reader, ctx context.Context) {
	prevData := []byte{}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var responseReader io.Reader
		var responseReaderSpy bytes.Buffer

		if len(prevData) > 0 {
			responseReader = io.MultiReader(bytes.NewBuffer(prevData), reader)
			prevData = []byte{}
		} else {
			responseReader = reader
		}

		responseReader = io.TeeReader(responseReader, &responseReaderSpy)
		buffResponseReader := bufio.NewReader(responseReader)

		unreadLast := func() {
			totalRead := responseReaderSpy.Len()
			tempBuffer := make([]byte, totalRead)
			responseReaderSpy.Read(tempBuffer)
			prevData = tempBuffer
		}

		resp, err := http.ReadResponse(buffResponseReader, nil)
		if err != nil {
			unreadLast()
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			unreadLast()
			continue
		}

		// we might still have some bytes in the buffer save for next round
		unreadBufferSize := buffResponseReader.Buffered()
		if unreadBufferSize > 0 {
			prevData = make([]byte, unreadBufferSize)
			buffResponseReader.Read(prevData)
		}

		MainReqResQueue.AddResponse(&WrappedResp{Body: body, Resp: resp})
	}
}

func logRequestResponse(wReq *WrappedReq, wResp *WrappedResp) {
	yellow := color.New(color.FgYellow).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	req := wReq.Req
	resp := wResp.Resp

	fmt.Printf("%s ", yellow(time.Now().Format("2006/01/02 - 15:04:05")))
	fmt.Printf("| %-7s %-30s", green(req.Method), green(req.URL))
	if resp.StatusCode >= 400 {
		fmt.Printf(" | %-3s", red(resp.StatusCode))
	} else {
		fmt.Printf(" | %-3s", cyan(resp.StatusCode))
	}

	body := wResp.Body
	if len(body) > 0 {
		bodyStr := string(body)
		fmt.Printf(" | %-50s", bodyStr)
	}

	fmt.Println()
}

func getTlsConfig() *tls.Config {
	return &tls.Config{}
}

func dialTls(address string) (net.Conn, error) {
	config := getTlsConfig()
	dialer := &tls.Dialer{Config: config}
	conn, err := dialer.Dial("tcp", address)
	return conn, err
}
