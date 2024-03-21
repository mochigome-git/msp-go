package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"nk2-PLCcapture-go/pkg/config"
	"nk2-PLCcapture-go/pkg/mqtt"
	"nk2-PLCcapture-go/pkg/plc"
	"nk2-PLCcapture-go/pkg/utils"

	jsoniter "github.com/json-iterator/go"
)

func main() {
	config.LoadEnv(".env.local")

	// Use os.Getenv() instead of getEnv()
	mqttHost := os.Getenv("MQTT_HOST")
	plcHost := os.Getenv("PLC_HOST")
	plcPort := config.GetEnvAsInt("PLC_PORT", 5011)
	devices16 := os.Getenv("DEVICES_16bit")
	devices32 := os.Getenv("DEVICES_32bit")

	// Set up a channel to listen for SIGTERM signals
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	// Create a logger to use for logging messages
	logger := log.New(os.Stdout, "", log.LstdFlags)

	// Connect to the MQTT server
	mqttclient := mqtt.NewMQTTClient(mqttHost, logger)
	defer mqttclient.Disconnect(250)

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

	// Combine the 16-bit and 32-bit devices into a single slice
	devices := append(devices16Parsed, devices32Parsed...)

	// Create a channel to signal when the main loop has finished
	doneCh := make(chan struct{})

	// Create a boolean variable to indicate whether to shutdown
	shutdown := false

	// Start a goroutine to listen for signals
	go func() {
		// Iterate over the signal channel
		for sig := range signalCh {
			// If a SIGINT or SIGTERM signal is received, set the shutdown variable to true
			if sig == syscall.SIGINT || sig == syscall.SIGTERM {
				shutdown = true
				break
			}
		}
	}()

	// Initialize the MSP client
	err = plc.InitMSPClient(plcHost, plcPort)
	if err != nil {
		logger.Fatalf("Failed to initialize MSP client: %v", err)
	} else {
		log.Printf("Start collecting data from %s", plcHost)
	}
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
				topic := "nk3/plc/holding_register/16bit&32bit/" + message["address"].(string)
				mqtt.PublishMessage(mqttclient, topic, string(messageJSON), logger)

			}
		}()
	}

	// Read data from devices and send it to the channel
	go func() {
		defer close(dataCh) // Close the dataCh channel to signal workers to complete

		for {
			// Read data from devices and send it to dataCh
			for _, device := range devices {

				value, err := plc.ReadData(device.DeviceType, device.DeviceNumber, device.NumberRegisters)
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

			// Check if the shutdown variable is true before reconnecting to the PLC
			if shutdown {
				break
			}

			// Check if all devices in devices32 are being read
			if len(devices) != len(devices32Parsed)+len(devices16Parsed) {
				logger.Printf("Number of devices read (%d) does not match the number of devices listed in DEVICES_32bit (%d) and DEVICES_16bit (%d).", len(devices), len(devices32Parsed), len(devices16Parsed))
				logger.Printf("Restarting the program...")
				panic("Device count mismatch")
			}
		}

	}()

	// Wait for either the main loop to finish or a signal to be received
	select {
	case <-signalCh:
		logger.Println("Program terminated by signal")
		shutdown = true
	case <-doneCh:
		logger.Println("Main loop completed")
	}

	// Disconnect from the MQTT server
	wg.Wait()
	close(signalCh) // Close the signalCh channel
	close(doneCh)   // Close the doneCh channel
	mqttclient.Disconnect(250)

	// Perform any necessary cleanup tasks and exit the program
	logger.Println("Exiting program...")
}
