package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"github.com/joho/godotenv"
)

type AppConfig struct {
	// PLC configure
	Profilling   int    // pprof server port
	PlcHost      string // plcHost stores the PLC's hostname
	PlcPort      int    // plcPort stores the PLC's port number
	FxStr        string // Mitsubishi PLC FX series true =1 false =0
	Devices16    string // store 16bit device for SLMP(Seamless Message Protocol) query
	Devices32    string // store 32bit device for SLMP(Seamless Message Protocol) query
	Devices2     string // store 2bit device for SLMP(Seamless Message Protocol) query
	DevicesAscii string // convert Ascii to text

	// MQTT Broker configure
	MqttHost      string // mqtthost stores the MQTT broker's hostname
	MqttTopic     string // topic stores the topic of the MQTT broker
	MqttsStr      string // Turn on for TLS connection
	MqttSkip      bool //Skip Mqtt use direct mode
	ECScaCert     string // ESC verion direct read from params store
	ECSclientCert string // ESC verion direct read from params store
	ECSclientKey  string // ESC verion direct read from params store
}


type PlcConfig struct {
	DestPlcHost         string // plcHost stores the PLC's hostname
	DestPlcPort         int    // plcPort stores the PLC's port number
	DestFxStr           string // Mitsubishi PLC FX series true =1 false =0
	DestPlcDevice       string
	DestPlcData         string
	DestPlcDeviceUpsert string
}


var Cfg AppConfig
var Plc PlcConfig

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

	Cfg = AppConfig{

		Profilling:    GetEnvAsInt("PPROFT_PORT", 6060),
		PlcHost:       os.Getenv("PLC_HOST"),
		PlcPort:       GetEnvAsInt("PLC_PORT", 5011),
		FxStr:         os.Getenv("PLC_MODEL"),
		Devices16:     os.Getenv("DEVICES_16bit"),
		Devices32:     os.Getenv("DEVICES_32bit"),
		Devices2:      os.Getenv("DEVICES_2bit"),
		MqttTopic:     os.Getenv("MQTT_TOPIC"),
		DevicesAscii:  os.Getenv("DEVICES_ASCII"),
		MqttHost:      os.Getenv("MQTT_HOST"),
		MqttsStr:      os.Getenv("MQTTS_ON"),
		ECScaCert:     os.Getenv("ECS_MQTT_CA_CERTIFICATE"),
		ECSclientCert: os.Getenv("ECS_MQTT_CLIENT_CERTIFICATE"),
		ECSclientKey:  os.Getenv("ECS_MQTT_PRIVATE_KEY"),
		MqttSkip: strings.ToLower(os.Getenv("MQTT_SKIP")) == "true",

	}

	Plc = PlcConfig{
		DestPlcHost:         os.Getenv("DEST_PLC_HOST"),
		DestPlcPort:         GetEnvAsInt("DEST_PLC_PORT", 5011),
		DestFxStr:           os.Getenv("DES_PLC_MODEL"),
		DestPlcDevice:       os.Getenv("DEST_PLC_DEVICE"),
		DestPlcData:         os.Getenv("DEST_PLC_DATA"),
		DestPlcDeviceUpsert: os.Getenv("DEST_PLC_DEVICE_UPSERT"),
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
