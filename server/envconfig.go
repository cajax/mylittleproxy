package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/cajax/mylittleproxy/appConfig"
	"github.com/cajax/mylittleproxy/tunnel"
)

// Environment variables the server reads. They override the config file, so one
// image can be deployed with different settings, and they can replace the file
// entirely.
const (
	envDebug          = "MYLITTLEPROXY_DEBUG"
	envListen         = "MYLITTLEPROXY_LISTEN"
	envListenControl  = "MYLITTLEPROXY_LISTEN_CONTROL"
	envSignatureKey   = "MYLITTLEPROXY_SIGNATURE_KEY"
	envAllowedHosts   = "MYLITTLEPROXY_ALLOWED_HOSTS"
	envAllowedClients = "MYLITTLEPROXY_ALLOWED_CLIENTS"
	envControlPath    = "MYLITTLEPROXY_CONTROL_PATH"
	envControlMethod  = "MYLITTLEPROXY_CONTROL_METHOD"
)

// applyEnv overlays the environment onto a config. A variable that is not set
// leaves the config's value alone.
func applyEnv(config *appConfig.Server) error {
	applyEnvString(envListen, &config.Listen)
	applyEnvString(envListenControl, &config.ListenControl)
	applyEnvString(envSignatureKey, &config.SignatureKey)
	applyEnvString(envControlPath, &config.ControlPath)
	applyEnvString(envControlMethod, &config.ControlMethod)

	if err := applyEnvBool(envDebug, &config.Debug); err != nil {
		return err
	}
	if err := applyEnvList(envAllowedHosts, &config.AllowedHosts); err != nil {
		return err
	}

	return applyEnvList(envAllowedClients, &config.AllowedClients)
}

func applyEnvString(name string, target *string) {
	if value, ok := os.LookupEnv(name); ok {
		*target = value
	}
}

func applyEnvBool(name string, target *bool) error {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%s must be a boolean, got %q", name, value)
	}

	*target = parsed

	return nil
}

// applyEnvList reads a list value. allowedHosts holds regexes and a regex may
// contain a comma, so a value starting with "[" is read as a JSON array;
// anything else is split on commas.
func applyEnvList(name string, target *[]string) error {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}

	value = strings.TrimSpace(value)

	if strings.HasPrefix(value, "[") {
		var parsed []string
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return fmt.Errorf("%s looks like a JSON array but cannot be read as one: %s", name, err)
		}

		*target = parsed

		return nil
	}

	parts := strings.Split(value, ",")
	list := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			list = append(list, trimmed)
		}
	}

	*target = list

	return nil
}

// loadConfig reads the config file if there is one, overlays the environment and
// checks that what the server cannot start without is present. A missing file is
// not an error when the environment supplies the rest; a file that cannot be
// parsed always is.
func loadConfig(path string) (appConfig.Server, error) {
	var config appConfig.Server

	err := tunnel.GetConfig(&path, &config)
	switch {
	case err == nil:
		log.Printf("Reading the configuration from %s, with the environment taking precedence", path)
	case errors.Is(err, fs.ErrNotExist):
		log.Printf("No config file at %s, reading the configuration from the environment", path)
	default:
		return config, fmt.Errorf("unable to read config %s: %s", path, err)
	}

	if err := applyEnv(&config); err != nil {
		return config, err
	}

	return config, checkRequiredSettings(config)
}

// checkRequiredSettings reports what the server cannot start without, naming the
// environment variable for it so a deployment with no config file can be fixed
// from the message alone.
func checkRequiredSettings(config appConfig.Server) error {
	var missing []string

	if config.Listen == "" {
		missing = append(missing, fmt.Sprintf("listen (%s)", envListen))
	}
	if config.SignatureKey == "" {
		missing = append(missing, fmt.Sprintf("signatureKey (%s)", envSignatureKey))
	}
	if len(config.AllowedHosts) == 0 {
		missing = append(missing, fmt.Sprintf("allowedHosts (%s)", envAllowedHosts))
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required settings: %s", strings.Join(missing, ", "))
	}

	return nil
}
