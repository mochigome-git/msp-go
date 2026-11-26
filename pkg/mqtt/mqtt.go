package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/mochigome-git/msp-go/internal/monitor"
	"github.com/mochigome-git/msp-go/pkg/config"
)

const (
	// connectWait is how long startup waits for the first connection.
	// After that the client keeps retrying in the background.
	connectWait = 15 * time.Second
	// publishTimeout bounds every publish so a broker outage can't block
	// the workers (and, through the unbuffered queue, the PLC reads).
	publishTimeout = 5 * time.Second
)

// NewClient creates and connects an MQTT client. It returns an error only
// for configuration problems (bad TLS material). If the broker is
// unreachable at startup the client keeps retrying in the background and
// publishes fail fast until it connects.
func NewClient(cfg config.AppConfig, logger *log.Logger) (MQTT.Client, error) {
	opts := MQTT.NewClientOptions().
		AddBroker(cfg.MqttHost).
		SetClientID(cfg.MqttClientID).
		SetKeepAlive(30 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetConnectionLostHandler(func(_ MQTT.Client, err error) {
			logger.Printf("MQTT connection lost: %v — reconnecting", err)
		}).
		SetReconnectingHandler(func(MQTT.Client, *MQTT.ClientOptions) {
			logger.Printf("MQTT reconnecting to %s", cfg.MqttHost)
		}).
		SetOnConnectHandler(func(MQTT.Client) {
			logger.Printf("Connected to MQTT server %s as %q", cfg.MqttHost, cfg.MqttClientID)
		})

	if useTLS(cfg) {
		tlsConfig, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts.SetTLSConfig(tlsConfig)
	}

	client := MQTT.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(connectWait) {
		logger.Printf("⚠️ MQTT server %s not reachable yet — retrying in background", cfg.MqttHost)
	} else if err := token.Error(); err != nil {
		logger.Printf("⚠️ MQTT connect failed: %v — retrying in background", err)
	}
	return client, nil
}

func useTLS(cfg config.AppConfig) bool {
	on, _ := strconv.ParseBool(strings.TrimSpace(cfg.MqttsStr))
	return on
}

// buildTLSConfig loads the CA and client certificate from PEM contents
// (ECS_MQTT_*) or, if those are empty, from files (MQTT_CA_CERTIFICATE,
// MQTT_CLIENT_CERTIFICATE, MQTT_PRIVATE_KEY).
func buildTLSConfig(cfg config.AppConfig) (*tls.Config, error) {
	caPEM, err := readPEM(cfg.ECScaCert, cfg.CaCertFile, "CA certificate")
	if err != nil {
		return nil, err
	}
	certPEM, err := readPEM(cfg.ECSclientCert, cfg.ClientCertFile, "client certificate")
	if err != nil {
		return nil, err
	}
	keyPEM, err := readPEM(cfg.ECSclientKey, cfg.ClientKeyFile, "private key")
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("MQTT CA certificate: no valid PEM certificate found")
		}
		tlsConfig.RootCAs = pool
	} // else: system roots

	switch {
	case len(certPEM) > 0 && len(keyPEM) > 0:
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("MQTT client certificate/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	case len(certPEM) > 0 || len(keyPEM) > 0:
		return nil, errors.New("MQTT client certificate and private key must be set together")
	}
	return tlsConfig, nil
}

func readPEM(contents, path, what string) ([]byte, error) {
	if strings.TrimSpace(contents) != "" {
		return []byte(contents), nil
	}
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("MQTT %s: %w", what, err)
	}
	return b, nil
}

// dropping is set while publishes are being dropped for lack of a
// connection, so the outage is logged once rather than per message.
var dropping atomic.Bool

// PublishMessage publishes with QoS 0 and waits at most publishTimeout.
// While the broker is disconnected, messages are dropped (QoS 0 semantics)
// rather than queued in memory for the length of the outage.
func PublishMessage(client MQTT.Client, topic string, message string, logger *log.Logger) {
	if !client.IsConnectionOpen() {
		if !dropping.Swap(true) {
			logger.Printf("MQTT not connected — dropping messages until it reconnects")
		}
		return
	}
	if dropping.Swap(false) {
		logger.Printf("MQTT connected — publishing again")
	}
	token := client.Publish(topic, 0, false, message)
	if !token.WaitTimeout(publishTimeout) {
		logger.Printf("Timeout publishing to topic %s", topic)
		return
	}
	if err := token.Error(); err != nil {
		logger.Printf("Error publishing message to topic %s: %s", topic, err)
		return
	}
	monitor.IncMessageCounter()
}
