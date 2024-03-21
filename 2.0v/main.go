package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"nk3-PLCcapture-go/pkg/config"
	"nk3-PLCcapture-go/pkg/mqtt"
	"nk3-PLCcapture-go/pkg/plc"
	"nk3-PLCcapture-go/pkg/utils"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	jsoniter "github.com/json-iterator/go"
)

var (
	mqttHost       string
	plcHost        string
	plcPort        int
	devices2       string
	devices16      string
	devices32      string
	mqttsStr       string
	mqttTopic      string
	caCertFile     string
	clientCertFile string
	clientKeyFile  string
)

func init() {
	//config.LoadEnv(".env.local")
	mqttsStr = os.Getenv("MQTTS_ON")
	mqttHost = os.Getenv("MQTT_HOST")
	plcHost = os.Getenv("PLC_HOST")
	plcPort = config.GetEnvAsInt("PLC_PORT", 5011)
	devices16 = os.Getenv("DEVICES_16bit")
	devices32 = os.Getenv("DEVICES_32bit")
	devices2 = os.Getenv("DEVICES_2bit")
	mqttTopic = os.Getenv("MQTT_TOPIC")
	caCertFile = os.Getenv("MQTT_CA_CERTIFICATE")
	clientCertFile = os.Getenv("MQTT_CLIENT_CERTIFICATE")
	clientKeyFile = os.Getenv("MQTT_PRIVATE_KEY")
}

func main() {

	// Create a logger to use for logging messages
	logger := log.New(os.Stdout, "", log.LstdFlags)

	// Parse the string value into a boolean, defaulting to false if parsing fails
	mqtts, _ := strconv.ParseBool(mqttsStr)

	var mqttclient MQTT.Client
	// Create MQTT client based on whether mqtts is true or false
	if mqtts {
		mqttclient = mqtt.NewMQTTClientWithTLS(mqttHost, caCertFile, clientCertFile, clientKeyFile, logger)
	} else {
		mqttclient = mqtt.NewMQTTClient(mqttHost, logger)
	}

	defer mqttclient.Disconnect(250)

	// Parse the device addresses for 2-bit devices
	devices2Parsed, err := utils.ParseDeviceAddresses(devices2, logger)
	if err != nil {
		logger.Fatalf("Error parsing device addresses: %v", err)
	}

	// Parse the device addresses for 16-bit devices
	devices16Parsed, err := utils.ParseDeviceAddresses(devices16, logger)
	if err != nil {
		logger.Fatalf("Error parsing device addresses: %v", err)
	}

	// Parse the device addresses for 32-bit devices
	devices32Parsed, err := utils.ParseDeviceAddresses(devices32, logger)
	if err != nil {
		logger.Fatalf("Error parsing device addresses: %v", err)
	}

	// Combine the 2-bit, 16-bit and 32-bit devices into a single slice
	devices := append(devices2Parsed, devices16Parsed...)
	devices = append(devices, devices32Parsed...)

	// Initialize the MSP client
	err = plc.InitMSPClient(plcHost, plcPort)
	if err != nil {
		logger.Fatalf("Failed to initialize MSP client: %v", err)
	} else {
		log.Printf("Start collecting data from %s", plcHost)
	}

	for {
		workerCount := 15
		// Use a buffered channel to store the data to be processed
		dataCh := make(chan map[string]interface{}, workerCount) // Buffered channel with capacity equal to the number of workers

		// Start the worker goroutines before reading data from the devices
		// Spawn multiple worker goroutines that read the data from the channel, process it, and send it to MQTT
		var wg sync.WaitGroup
		wg.Add(workerCount)

		for i := 0; i < workerCount; i++ {
			go func() {
				defer wg.Done()
				for message := range dataCh {

					// Convert the message to a JSON string
					messageJSON, err := jsoniter.Marshal(message)
					if err != nil {
						logger.Printf("Error marshaling message to JSON:%s", err)
						continue
					}

					// Publish the message to the MQTT server
					topic := mqttTopic + message["address"].(string)
					mqtt.PublishMessage(mqttclient, topic, string(messageJSON), logger)
				}
			}()
		}

		// Run the main loop in a separate goroutine
		go func() {
			for {

				// Initialize the context with a timeout of 20 seconds
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()

				// Read data from devices and send it to dataCh
				for _, device := range devices {
					select {
					case <-ctx.Done():
						logger.Printf("%s timed out. error: %s\n", device.DeviceType+strconv.Itoa(int(device.DeviceNumber)), ctx.Err())
						logger.Println("Program terminated by os.Exit")
						os.Exit(1)
						return
					default:
						value, err := ReadDataWithContext(ctx, device.DeviceType, device.DeviceNumber, device.NumberRegisters)
						if err != nil {
							logger.Printf("Error reading data from PLC for device %s: %s", device.DeviceType+strconv.Itoa(int(device.DeviceNumber)), err)
							break
						}
						message := map[string]interface{}{
							"address": device.DeviceType + strconv.Itoa(int(device.DeviceNumber)),
							"value":   value,
						}
						dataCh <- message
					}
				}

			}
		}()

		<-dataCh

		// dataCh is closed, all workers are done
		wg.Wait()

	}

}

func ReadDataWithContext(ctx context.Context, deviceType string, deviceNumber uint16, numRegisters uint16) (value interface{}, err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Perform the actual data reading operation
		value, err = plc.ReadData(ctx, deviceType, deviceNumber, numRegisters)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
}
