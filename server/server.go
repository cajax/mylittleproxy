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
	flag.Parse()
	var config appConfig.Server
	err := tunnel.GetConfig(configPath, &config)

	if err != nil {
		log.Printf("Unable to read config: %s", err)
		os.Exit(1)
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
	logger.Info("Listening", zap.String("control_url", config.Listen+cfg.ControlPath), zap.String("control_method", cfg.ControlMethod))
	err = newHTTPServer(config.Listen, server).ListenAndServe()
	if err != nil {
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
