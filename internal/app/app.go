package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/mochigome-git/msp-go/internal/app/profiler"
	"github.com/mochigome-git/msp-go/internal/app/worker"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"
	"github.com/mochigome-git/msp-go/pkg/plc"
	"github.com/mochigome-git/msp-go/pkg/utils"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type Application struct {
	cfg        config.AppConfig
	logger     *log.Logger
	mqttClient MQTT.Client
	devices    []utils.Device
	workerPool *worker.Pool
	fx         bool
}

func NewApplication(cfg config.AppConfig, logger *log.Logger) (*Application, error) {
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
	devices := make([]utils.Device, 0)
	sources := []string{cfg.Devices2, cfg.Devices16, cfg.Devices32, cfg.DevicesAscii}
	for _, source := range sources {
		parsed, err := utils.ParseDeviceAddresses(source, logger)
		if err != nil {
			logger.Printf("Error parsing device list: %v", err)
		}
		devices = append(devices, parsed...)
	}

	// Init PLC
	if err := plc.InitMSPClient(cfg.PlcHost, cfg.PlcPort); err != nil {
		return nil, fmt.Errorf("init PLC failed: %w", err)
	}
	logger.Printf("Start collecting data from %s", cfg.PlcHost)

	return &Application{
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
		devices:    devices,
		fx:         fx,
	}, nil
}

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
