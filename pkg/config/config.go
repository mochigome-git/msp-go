package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	// App-level
	PprofPort int // pprof port on 127.0.0.1; 0 = disabled (PPROF_PORT)

	// Polling
	PollInterval time.Duration // pause between read cycles (POLL_INTERVAL_MS)

	// MQTT
	MqttHost     string // broker URL, e.g. tcp://host:1883 or mqtts://host:8883
	MqttTopic    string // topic prefix; the device address is appended
	MqttsStr     string // MQTTS_ON: "true" enables TLS
	MqttSkip     bool   // MQTT_SKIP: no MQTT, write values to the PLCs directly
	MqttClientID string // MQTT_CLIENT_ID; default msp-go-<hostname>
	// MQTT_TOPIC_INCLUDE_SOURCE: publish to <topic><plc>/<address> instead of
	// <topic><address>, so the same address on two PLCs gets separate topics.
	MqttTopicIncludeSource bool
	// PUBLISH_ON_CHANGE: publish a value only when it changed, plus a full
	// refresh every PublishRefresh (PUBLISH_REFRESH_SEC, default 60s).
	PublishOnChange bool
	PublishRefresh  time.Duration

	// TLS material, as PEM contents (ECS_MQTT_*; e.g. from a parameter
	// store) or as file paths (MQTT_CA_CERTIFICATE, ...). Contents win.
	ECScaCert      string
	ECSclientCert  string
	ECSclientKey   string
	CaCertFile     string
	ClientCertFile string
	ClientKeyFile  string

	// PLC-level
	PLCs []PLCConfig
}

type PLCConfig struct {
	Name         string // "main", "secondary"
	Host         string // PLC hostname or IP
	Port         int    // PLC port
	FxModel      bool   // Mitsubishi FX series (1E frame)
	Devices2     string // DEVICES_2bit
	Devices16    string // DEVICES_16bit
	Devices32    string // DEVICES_32bit
	DevicesAscii string // DEVICES_ASCII
	WriteMap     string // values written INTO this PLC: "SRC>TYPE,NUM,WRITETYPE,REGS;..."
	CondMap      string // conditions guarding WriteMap: "BIT==SRC;BIT!=SRC"
	Brand        string // "mitsubishi" (default) or "shibaura"

	// Block reads (Mitsubishi 3E only): group devices into batch reads.
	BlockRead   bool  // BLOCK_READ / SEC_BLOCK_READ
	BlockMaxGap int64 // BLOCK_MAX_GAP: unused words allowed inside a block (0 = default 128)

	// TextEncoding for data type 4 registers: "auto" (ASCII + Shift-JIS, default)
	// or "unicode" (UTF-16). TEXT_ENCODING / SEC_TEXT_ENCODING.
	TextEncoding string
}

var Cfg AppConfig

// Load reads configuration from the given env files (if present) and the
// environment. It returns an error listing every variable that is set but
// cannot be parsed, instead of silently using the default.
func Load(files ...string) error {
	for _, file := range files {
		if err := godotenv.Load(file); err != nil {
			log.Printf("Info: %s not loaded (%v), using system environment", file, err)
		}
	}

	var errs []error
	envInt := func(name string, def int) int {
		v, err := GetEnvAsInt(name, def)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}
	envBool := func(name string, def bool) bool {
		v, err := GetEnvAsBool(name, def)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}

	mainFx := envBool("PLC_MODEL", false)
	mainPLC := PLCConfig{
		Name:         "main",
		Host:         os.Getenv("PLC_HOST"),
		Port:         envInt("PLC_PORT", 5011),
		FxModel:      mainFx,
		Devices2:     os.Getenv("DEVICES_2bit"),
		Devices16:    os.Getenv("DEVICES_16bit"),
		Devices32:    os.Getenv("DEVICES_32bit"),
		DevicesAscii: os.Getenv("DEVICES_ASCII"),
		WriteMap:     os.Getenv("WRITE_MAP_SEC_TO_PRIM"),
		CondMap:      os.Getenv("WRITE_MAP_SEC_TO_PRIM_CONDITION"),
		Brand:        strings.ToLower(strings.TrimSpace(os.Getenv("MAIN_PLC_BRAND"))),
		BlockRead:    envBool("BLOCK_READ", false),
		BlockMaxGap:  int64(envInt("BLOCK_MAX_GAP", 0)),
		TextEncoding: os.Getenv("TEXT_ENCODING"),
	}

	secondaryPLC := PLCConfig{
		Name: "secondary",
		Host: os.Getenv("SEC_PLC_HOST"),
		Port: envInt("SEC_PLC_PORT", 5011),
		// SEC_PLC_MODEL; falls back to PLC_MODEL so existing setups where
		// both PLCs are FX keep working.
		FxModel:      envBool("SEC_PLC_MODEL", mainFx),
		Devices2:     os.Getenv("SEC_DEVICES_2bit"),
		Devices16:    os.Getenv("SEC_DEVICES_16bit"),
		Devices32:    os.Getenv("SEC_DEVICES_32bit"),
		DevicesAscii: os.Getenv("SEC_DEVICES_ASCII"),
		WriteMap:     os.Getenv("WRITE_MAP_PRIM_TO_SEC"),
		CondMap:      os.Getenv("WRITE_MAP_PRIM_TO_SEC_CONDITION"),
		Brand:        strings.ToLower(strings.TrimSpace(os.Getenv("SUB_PLC_BRAND"))),
		BlockRead:    envBool("SEC_BLOCK_READ", false),
		BlockMaxGap:  int64(envInt("SEC_BLOCK_MAX_GAP", 0)),
		TextEncoding: os.Getenv("SEC_TEXT_ENCODING"),
	}

	pprofPort := 0
	if _, ok := os.LookupEnv("PPROF_PORT"); ok {
		pprofPort = envInt("PPROF_PORT", 0)
	} else if _, ok := os.LookupEnv("PPROFT_PORT"); ok { // legacy misspelling
		pprofPort = envInt("PPROFT_PORT", 0)
	}

	clientID := strings.TrimSpace(os.Getenv("MQTT_CLIENT_ID"))
	if clientID == "" {
		host, _ := os.Hostname()
		clientID = "msp-go-" + host
	}

	pollMs := envInt("POLL_INTERVAL_MS", 0)
	refreshSec := envInt("PUBLISH_REFRESH_SEC", 60)
	if pollMs < 0 {
		errs = append(errs, fmt.Errorf("POLL_INTERVAL_MS must be >= 0, got %d", pollMs))
	}
	if refreshSec < 0 {
		errs = append(errs, fmt.Errorf("PUBLISH_REFRESH_SEC must be >= 0, got %d", refreshSec))
	}

	Cfg = AppConfig{
		PprofPort:              pprofPort,
		PollInterval:           time.Duration(pollMs) * time.Millisecond,
		MqttHost:               os.Getenv("MQTT_HOST"),
		MqttTopic:              os.Getenv("MQTT_TOPIC"),
		MqttsStr:               os.Getenv("MQTTS_ON"),
		MqttSkip:               strings.EqualFold(strings.TrimSpace(os.Getenv("MQTT_SKIP")), "true"),
		MqttClientID:           clientID,
		MqttTopicIncludeSource: envBool("MQTT_TOPIC_INCLUDE_SOURCE", false),
		PublishOnChange:        envBool("PUBLISH_ON_CHANGE", false),
		PublishRefresh:         time.Duration(refreshSec) * time.Second,
		ECScaCert:              os.Getenv("ECS_MQTT_CA_CERTIFICATE"),
		ECSclientCert:          os.Getenv("ECS_MQTT_CLIENT_CERTIFICATE"),
		ECSclientKey:           os.Getenv("ECS_MQTT_PRIVATE_KEY"),
		CaCertFile:             os.Getenv("MQTT_CA_CERTIFICATE"),
		ClientCertFile:         os.Getenv("MQTT_CLIENT_CERTIFICATE"),
		ClientKeyFile:          os.Getenv("MQTT_PRIVATE_KEY"),
		PLCs:                   []PLCConfig{mainPLC, secondaryPLC},
	}

	if !Cfg.MqttSkip && strings.TrimSpace(Cfg.MqttHost) == "" {
		errs = append(errs, errors.New("MQTT_HOST is empty (set it, or MQTT_SKIP=true for direct PLC-to-PLC mode)"))
	}
	return errors.Join(errs...)
}

// GetEnvAsInt returns the variable as an int, or defaultValue if it is
// unset or empty. A value that is set but not an integer is an error.
func GetEnvAsInt(name string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue, nil
	}
	i, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, fmt.Errorf("%s=%q is not an integer", name, value)
	}
	return i, nil
}

// GetEnvAsBool returns the variable as a bool, or defaultValue if it is
// unset or empty. A value that is set but not a bool is an error.
func GetEnvAsBool(name string, defaultValue bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue, nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue, fmt.Errorf("%s=%q is not true/false", name, value)
	}
	return b, nil
}
