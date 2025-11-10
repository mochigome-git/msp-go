package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	// App-level
	Profilling    int    // pprof server port
	MqttHost      string // mqtthost stores the MQTT broker's hostname
	MqttTopic     string // topic stores the topic of the MQTT broker
	MqttsStr      string // Turn on for TLS connection
	MqttSkip      bool   //Skip Mqtt use direct mode
	ECScaCert     string // ESC verion direct read from params store
	ECSclientCert string // ESC verion direct read from params store
	ECSclientKey  string // ESC verion direct read from params store

	// PLC-level
	PLCs []PLCConfig
}

type PLCConfig struct {
	Name         string // "main", "secondary", "backup"
	Host         string // plcHost stores the PLC's hostname
	Port         int    // plcPort stores the PLC's port number
	FxModel      string // Mitsubishi PLC FX series true =1 false =0
	Devices2     string // store 2bit device for SLMP(Seamless Message Protocol) query
	Devices16    string // store 16bit device for SLMP(Seamless Message Protocol) query
	Devices32    string // store 32bit device for SLMP(Seamless Message Protocol) query
	DevicesAscii string // convert Ascii to text
	DeviceUpsert string
	Data         string
	WriteMap     string
	CondMap      string // store conditional rules, e.g., "M64==D71,M30!=D80"
}

var Cfg AppConfig
var Plc PLCConfig

// Load initializes all configuration variables from environment variables
func Load(files ...string) {
	// Try to load from the specified file first
	if len(files) > 0 {
		for _, file := range files {
			err := godotenv.Load(file)
			if err != nil {
				log.Printf("Info: %s not found or failed to load local.env, falling back to system environment", file)
			}
		}
	}

	mainPLC := PLCConfig{
		Name:         "main",
		Host:         os.Getenv("PLC_HOST"),
		Port:         GetEnvAsInt("PLC_PORT", 5011),
		FxModel:      os.Getenv("PLC_MODEL"),
		Devices2:     os.Getenv("DEVICES_2bit"),
		Devices16:    os.Getenv("DEVICES_16bit"),
		Devices32:    os.Getenv("DEVICES_32bit"),
		DevicesAscii: os.Getenv("DEVICES_ASCII"),
		WriteMap:     os.Getenv("WRITE_MAP_SEC_TO_PRIM"),
		CondMap:      os.Getenv("WRITE_MAP_SEC_TO_PRIM_CONDITION"),
	}

	secondaryPLC := PLCConfig{
		Name:         "secondary",
		Host:         os.Getenv("SEC_PLC_HOST"),
		Port:         GetEnvAsInt("SEC_PLC_PORT", 5011),
		FxModel:      os.Getenv("SEC_PLC_MODEL"),
		Devices2:     os.Getenv("SEC_DEVICES_2bit"),
		Devices16:    os.Getenv("SEC_DEVICES_16bit"),
		Devices32:    os.Getenv("SEC_DEVICES_32bit"),
		DevicesAscii: os.Getenv("SEC_DEVICES_ASCII"),
		WriteMap:     os.Getenv("WRITE_MAP_PRIM_TO_SEC"),
		CondMap:      os.Getenv("WRITE_MAP_PRIM_TO_SEC_CONDITION"),
	}

	Cfg = AppConfig{
		Profilling:    GetEnvAsInt("PPROFT_PORT", 6060),
		MqttHost:      os.Getenv("MQTT_HOST"),
		MqttTopic:     os.Getenv("MQTT_TOPIC"),
		MqttsStr:      os.Getenv("MQTTS_ON"),
		MqttSkip:      strings.ToLower(os.Getenv("MQTT_SKIP")) == "true",
		ECScaCert:     os.Getenv("ECS_MQTT_CA_CERTIFICATE"),
		ECSclientCert: os.Getenv("ECS_MQTT_CLIENT_CERTIFICATE"),
		ECSclientKey:  os.Getenv("ECS_MQTT_PRIVATE_KEY"),
		PLCs:          []PLCConfig{mainPLC, secondaryPLC},
	}
}

// GetEnvAsInt gets the value of an environment variable as a uint16
func GetEnvAsInt(name string, defaultValue int) int {
	if value, exists := os.LookupEnv(name); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
