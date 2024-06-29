package cli

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samuelships/harlot/client"
	"github.com/samuelships/harlot/server"
	"github.com/samuelships/harlot/utils"
)

var (
	// client
	clientStartCmd    = flag.NewFlagSet("start", flag.ExitOnError)
	clientRegisterCmd = flag.NewFlagSet("register", flag.ExitOnError)
	clientLoginCmd    = flag.NewFlagSet("login", flag.ExitOnError)

	// server
	serverStartCmd = flag.NewFlagSet("start", flag.ExitOnError)
)

func RunCommand() {
	if len(os.Args) < 2 {
		PrintHelp()
		os.Exit(1)
	}

	// client start
	protocol := clientStartCmd.String("protocol", "http", "Protocol to use for the tunnel. Valid options are 'http', 'https', 'tcp', 'tls'")
	port := clientStartCmd.Int("port", 80, "Local port from which traffic will be tunneled to")
	subdomain := clientStartCmd.String("subdomain", "one", "External subdomain to bind service on")
	clientStartServerUrl := clientStartCmd.String("serverUrl", "harlot.app:8050", "Server url to connect to")

	// client register
	serverUrl := clientRegisterCmd.String("serverUrl", "harlot.app", "Server to authenticate with")

	// client login
	token := clientLoginCmd.String("token", "===", "The auth token obtained from eginstration")
	loginServerUrl := clientLoginCmd.String("serverUrl", "harlot.app:8050", "Server to authenticate with")

	if len(os.Args) < 3 {
		PrintHelp()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "client":
		switch os.Args[2] {
		case "register":
			clientRegisterCmd.Parse(os.Args[3:])
			HandleClientRegisterCommand(*serverUrl)
		case "login":
			clientLoginCmd.Parse(os.Args[3:])
			HandleClientLoginCommand(*loginServerUrl, *token)
		case "start":
			clientStartCmd.Parse(os.Args[3:])
			HandleClientStartCommand(*protocol, *port, *subdomain, *clientStartServerUrl)
		default:
			PrintHelp()
			os.Exit(1)
		}
	case "server":
		switch os.Args[2] {
		case "start":
			serverStartCmd.Parse(os.Args[3:])
			HandleServerStartCommand()
		default:
			PrintHelp()
			os.Exit(1)
		}

	default:
		PrintHelp()
		os.Exit(1)
	}
}

func PrintHelp() {
	fmt.Println(`[x]---<=*=>---[x]
Harlot CLI - Command Line Interface for Harlot Tunneling Service

Usage:
  harlot [command]

Available Commands:
  client register       Registers the client with the Harlot server to obtain a token.
  client start          Starts a tunnel for specified protocol and port.
  client login          Logs the client into the harlot server using the provided token.
  server start          Starts the tunnel server.

Use "harlot help [command]" for more information about a command.
	`)
}

func HandleClientRegisterCommand(serverUrl string) {
	utils.LogInfo("Connecting to harlot server...")
	client, err := client.NewClient(serverUrl)
	if err != nil {
		panic(utils.LogErrorReturn("Failed to create client %w", err))
	}

	token, err := client.Register()
	if err != nil {
		panic(err)
	}

	utils.LogInfo("Registration successful", slog.String("token", token))
	utils.LogInfo("Use Login command to persist token")
}

var validProtocols = map[string]string{
	"http":  "http",
	"https": "https",
	"tcp":   "tcp",
	"tcps":  "tcps",
}

func HandleClientStartCommand(protocol string, port int, subdomain string, serverUrl string) {
	// validate protocol
	if _, ok := validProtocols[protocol]; !ok {
		PrintHelp()
		return
	}

	cl, err := client.NewClient(serverUrl)
	if err != nil {
		panic(utils.LogErrorReturn("Failed to create client %w", err))
	}

	token, err := client.GetTokenFromConfig()
	if err != nil {
		panic(utils.LogErrorReturn("Failed to create client %v", err))
	}

	ok, err := cl.Login(serverUrl, token)
	if err != nil {
		panic(utils.LogErrorReturn("Failed to create client %v", err))
	}

	if !ok {
		utils.LogError("There was an error logging into server")
		return
	}

	cl, err = cl.FromOld()
	if err != nil {
		panic(utils.LogErrorReturn("Failed to create client %w", err))
	}

	cl.Tunnel(serverUrl, token, subdomain, protocol, strings.HasSuffix(protocol, "s"), port)
}

func HandleClientLoginCommand(serverUrl, token string) {
	utils.LogInfo("Connecting to harlot server...")

	cl, err := client.NewClient(serverUrl)
	if err != nil {
		panic(utils.LogErrorReturn("Failed to create client %w", err))
	}

	utils.LogInfo("Authenticating with server...")
	ok, err := cl.Login(serverUrl, token)
	if !ok {
		utils.LogError("Authentication failed please provide a valid token")
		return
	}

	err = client.PersistToken(token)
	if err != nil {
		utils.LogError("Failed to persist token %w", err)
	}

	utils.LogInfo("Successfully authenticated with server")
}

func HandleServerStartCommand() {
	go func() {
		server.MainConnectionPooler.StartPrunner()
	}()

	privateServerPort := 8050
	publicServerPort := 443

	utils.LogInfo(fmt.Sprintf("Starting public server on : %d", publicServerPort))
	utils.LogInfo(fmt.Sprintf("Starting private server on : %d", privateServerPort))

	privateServer, err := server.CreateTlsServer(
		privateServerPort,
		server.PrivateServerHandler,
	)

	if err != nil {
		panic(err)
	}

	go func() {
		privateServer.Start()
	}()

	publicServer, err := server.CreatePlainServer(
		publicServerPort,
		server.PublicServerHandler,
	)

	if err != nil {
		panic(err)
	}

	go func() {
		publicServer.Start()
	}()

	for {
		time.Sleep(1 * time.Second)
		select {
		case <-privateServer.Done:
			return
		case <-publicServer.Done:
			return
		default:
		}
	}
}
