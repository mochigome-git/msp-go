package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/mochigome-git/msp-go/internal/app/profiler"
	"github.com/mochigome-git/msp-go/internal/app/worker"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"
	"github.com/mochigome-git/msp-go/pkg/plc"
	PLC "github.com/mochigome-git/msp-go/pkg/plc"
	MCP "github.com/mochigome-git/msp-go/pkg/mcp"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// Application is the main app handling both reading (MQTT → PLC) and writing (PLC → MQTT)
type Application struct {
	cfg        config.AppConfig
	cfgPlc     config.PlcConfig
	logger     *log.Logger
	mqttClient MQTT.Client
	devices    []PLC_Utils.Device
	workerPool *worker.Pool
	fx         bool
	client     MCP.Client
}

// NewApplication initializes MQTT, PLC, devices, and returns the app instance
func NewApplication(cfg config.AppConfig, cfgPlc config.PlcConfig, logger *log.Logger) (*Application, error) {
	mqtts, _ := strconv.ParseBool(cfg.MqttsStr)

	var mqttClient MQTT.Client
	if mqtts {
		mqttClient = mqtt.ECSNewMQTTClientWithTLS(cfg, logger)
	} else {
		mqttClient = mqtt.NewMQTTClient(cfg.MqttHost, logger)
	}

	// Parse fx
	fx, err := strconv.ParseBool(cfg.FxStr)
	if err != nil || cfg.FxStr == "fx" {
		fx = (cfg.FxStr == "fx")
		logger.Printf("Error parsing fx, fallback to: %v", fx)
	}

	// Parse devices
	devices := make([]PLC_Utils.Device, 0)
	sources := []string{cfg.Devices2, cfg.Devices16, cfg.Devices32, cfg.DevicesAscii}
	for _, source := range sources {
		parsed, err := PLC_Utils.ParseDeviceAddresses(source, logger)
		if err != nil {
			logger.Printf("Error parsing device list: %v", err)
		}
		devices = append(devices, parsed...)
	}

	// Init PLC connection
	if err := PLC.InitMSPClient(cfgPlc.DestPlcHost, cfgPlc.DestPlcPort); err != nil {
		return nil, fmt.Errorf("init PLC failed: %w", err)
	}
	logger.Printf("Start collecting data from %s", cfgPlc.DestPlcHost)

	return &Application{
		cfgPLc:     cfgPLC,
		logger:     logger,
		mqttClient: mqttClient,
		devices:    devices,
		fx:         fx,
	}, nil
}

// Run starts the profiler, worker pool, and data collection loop
func (a *Application) Run(ctx context.Context) error {
	defer a.mqttClient.Disconnect(250)

	go profiler.Start(a.cfg.Profilling, a.logger)

	a.workerPool = worker.NewPool(15, a.cfg, a.logger, a.mqttClient)
	a.workerPool.Start()
	defer a.workerPool.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.Println("Shutdown signal received")
			return nil
		default:
			a.readAndEnqueue(ctx)
		}
	}
}


func (a *Application) readAndEnqueue(ctx context.Context) {
	for _, device := range a.devices {
		// per-device timeout
		devCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

		val, err := readDataWithContext(devCtx, device, a.fx)
		cancel() // release resources as soon as read is done

		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				a.logger.Printf("Timeout reading %s, skipping", device.DeviceType+device.DeviceNumber)
				continue
				// uncomment this when running in docker container (comment continue)
				// os.Exit(1)

			}
			if errors.Is(err, context.Canceled) {
				a.logger.Printf("Read from %s canceled", device.DeviceType+device.DeviceNumber)
				continue
			}
			a.logger.Printf("Failed to read from %s: %v", device.DeviceType+device.DeviceNumber, err)
			continue
		}

		msg := map[string]any{
			"address": device.DeviceType + device.DeviceNumber,
			"value":   val,
		}
		a.workerPool.Enqueue(msg)
	}
}



func readDataWithContext(ctx context.Context, device utils.Device, fx bool) (value any, err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Perform the actual data reading operation
		value, err = plc.ReadData(ctx, device.DeviceType, device.DeviceNumber, device.NumberRegisters, fx)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
}

// PLC Write Operations


// Close cleanly disconnects PLC client
func (a *Application) Close() error {
	if a.client == nil {
		return nil
	}
	if err := a.client.Close(); err != nil {
		return fmt.Errorf("failed to close PLC connection: %w", err)
	}
	a.logger.Println("PLC connection closed")
	return nil
}

// writeDataWithContext writes bytes to PLC with context timeout
func (a *Application) writeDataWithContext(ctx context.Context, device PLC_Utils.Device, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return plc.BatchWrite(device.DeviceType, device.DeviceNumber, data, device.NumberRegisters, a.logger)
	}
}

// WritePLC writes data to PLC (single or batch)
func (a *Application) WritePLC(ctx context.Context, deviceStr string, value any) error {
	parts := strings.Split(deviceStr, ",")
	if len(parts) != 4 {
		return fmt.Errorf("invalid device string format, expected 'Type,Number,ProcessNumber,Registers'")
	}

	deviceType := parts[0]
	deviceNumber := parts[1]

	processNumber, err := strconv.Atoi(parts[2])
	if err != nil {
		return fmt.Errorf("invalid processNumber: %w", err)
	}

	numberRegisters, err := strconv.Atoi(parts[3])
	if err != nil {
		return fmt.Errorf("invalid numberRegisters: %w", err)
	}

	writeOne := func(valStr string) error {
		data, err := plc.EncodeData(valStr, processNumber)
		if err != nil {
			return err
		}

		device := PLC_Utils.Device{
			DeviceType:      deviceType,
			DeviceNumber:    deviceNumber,
			NumberRegisters: uint16(numberRegisters),
		}

		a.logger.Printf("Writing to %s%s: % X", deviceType, deviceNumber, data)

		if err := a.writeDataWithContext(ctx, device, data); err != nil {
			return fmt.Errorf("failed to write PLC data: %w", err)
		}
		return nil
	}

	switch v := value.(type) {
	case string:
		return writeOne(v)
	case []string:
		for _, val := range v {
			if err := writeOne(val); err != nil {
				return err
			}
		}
		return nil
	case bool:
		return writeOne(fmt.Sprintf("%t", v))
	case int, int8, int16, int32, int64:
		return writeOne(fmt.Sprintf("%d", v))
	case uint, uint8, uint16, uint32, uint64:
		return writeOne(fmt.Sprintf("%d", v))
	case float32, float64:
		return writeOne(fmt.Sprintf("%f", v))
	default:
		return fmt.Errorf("unsupported value type: %T", value)
	}
}