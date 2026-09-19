package main

import (
	"flag"
	"fmt"
	"github.com/cajax/mylittleproxy/appConfig"
	"github.com/cajax/mylittleproxy/proto"
	"github.com/cajax/mylittleproxy/tunnel"
	"go.uber.org/zap"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	configPath := flag.String("c", tunnel.GetExecutableDir()+string(os.PathSeparator)+"config.json", "Path to server config file")

	generate := flag.Bool("generate-client-config", false,
		"Print a client config derived from this server's config to stdout and exit")
	genOpts := clientConfigOptions{}
	flag.StringVar(&genOpts.identifier, "identifier", "", "Client identifier for -generate-client-config")
	flag.StringVar(&genOpts.domain, "domain", "", "Domain the client will serve, for -generate-client-config")
	flag.StringVar(&genOpts.target, "target", "", "Local server the client proxies to, for -generate-client-config (default "+defaultGeneratedTarget+")")
	flag.StringVar(&genOpts.address, "client-address", "",
		"Address clients should dial, for -generate-client-config. Required when the server listens on every interface")
	flag.BoolVar(&genOpts.omitSignatureKey, "omit-signature-key", false,
		"Leave signatureKey out of the generated config, for clients that read MYLITTLEPROXY_SIGNATURE_KEY")
	flag.BoolVar(&genOpts.debug, "client-debug", false, "Set debug in the generated client config")

	flag.Parse()
	var config appConfig.Server
	err := tunnel.GetConfig(configPath, &config)

	if err != nil {
		log.Printf("Unable to read config: %s", err)
		os.Exit(1)
	}

	if *generate {
		if err := generateClientConfig(config, genOpts, os.Stdout); err != nil {
			log.Printf("Unable to generate client config: %s", err)
			os.Exit(1)
		}
		return
	}

	var logger *zap.Logger
	if config.Debug {
		logger = zap.Must(zap.NewDevelopment())
	} else {
		logger = zap.Must(zap.NewProduction())
	}
	defer logger.Sync()

	fmt.Println("Running server with ", *configPath)

	signatureKey := getSignatureKey(config, logger)

	controlPath := proto.DefaultControlPath
	if config.ControlPath != "" {
		controlPath = config.ControlPath
	}

	controlMethod := proto.DefaultControlMethod
	if config.ControlMethod != "" {
		controlMethod = config.ControlMethod
	}

	cfg := &tunnel.ServerConfig{
		SignatureKey:   signatureKey,
		AllowedHosts:   config.AllowedHosts,
		AllowedClients: config.AllowedClients,
		Log:            logger,
		ControlPath:    controlPath,
		ControlMethod:  controlMethod,
	}
	server, err := tunnel.NewServer(cfg)
	if err != nil {
		logger.Fatal("unable to initialize tunnel server", zap.Error(err))
	}

	if err := checkListenAddresses(config.Listen, config.ListenControl); err != nil {
		logger.Fatal("invalid listen configuration", zap.Error(err))
	}

	controlAddress := config.ListenControl
	if controlAddress == "" {
		controlAddress = config.Listen
		logger.Warn("The control protocol is served on the public listener. Set listenControl to a private address to keep it off the public network",
			zap.String("listen", config.Listen))
	}

	logger.Info("Listening",
		zap.String("public_address", config.Listen),
		zap.String("control_url", controlAddress+cfg.ControlPath),
		zap.String("control_method", cfg.ControlMethod))

	if err := serve(config.Listen, config.ListenControl, server); err != nil {
		logger.Fatal("unable to start http server", zap.Error(err))
	}
}

// Timeouts for the public listener. ReadTimeout and WriteTimeout are left unset
// on purpose: they would cap the duration of a tunnelled request or response and
// break large or streaming bodies. ReadHeaderTimeout bounds how long a client
// may take to send its headers, and IdleTimeout reaps connections that are kept
// open without being used.
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 2 * time.Minute
)

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func getSignatureKey(config appConfig.Server, logger *zap.Logger) string {
	signatureKey := config.SignatureKey
	if signatureKey == "" {
		signatureKey = os.Getenv("MYLITTLEPROXY_SIGNATURE_KEY")
	}
	if signatureKey == "" {
		logger.Error("signature key must no be empty. Aborting")
		os.Exit(1)
	}
	return signatureKey
}
