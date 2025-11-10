package app

import (
	"context"
	"log"
	"strconv"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/mochigome-git/msp-go/internal/profiler"
	"github.com/mochigome-git/msp-go/internal/worker"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"

	"github.com/mochigome-git/msp-go/internal/plcservice"
)

// Application orchestrates MQTT, worker pool, and PLC service
type Application struct {
	cfg        config.AppConfig
	logger     *log.Logger
	mqttClient MQTT.Client
	workerPool *worker.Pool
	fx         bool

	plcSvc *plcservice.Service
}

// NewApplication initializes MQTT, PLC service, and devices
func NewApplication(cfg config.AppConfig, logger *log.Logger) (*Application, error) {
	var mqttClient MQTT.Client
	if !cfg.MqttSkip {
		mqtts, _ := strconv.ParseBool(cfg.MqttsStr)
		if mqtts {
			mqttClient = mqtt.ECSNewMQTTClientWithTLS(cfg, logger)
		} else {
			mqttClient = mqtt.NewMQTTClient(cfg.MqttHost, logger)
		}
		logger.Println("MQTT client initialized")
	} else {
		logger.Println("⚠️ MQTT initialization skipped (MQTT_SKIP=true)")
	}

	fx := len(cfg.PLCs) > 0 && cfg.PLCs[0].FxModel == "fx"

	plcSvc := plcservice.NewService(logger)
	for _, plcCfg := range cfg.PLCs {
		devices := []string{plcCfg.Devices2, plcCfg.Devices16, plcCfg.Devices32, plcCfg.DevicesAscii}
		if err := plcSvc.InitPLC(plcCfg.Name, plcCfg.Host, plcCfg.Port, devices, fx); err != nil {
			return nil, err
		}
	}

	return &Application{
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
		plcSvc:     plcSvc,
		fx:         fx,
	}, nil
}

// Run starts profiler, worker pool, and the PLC read loop
func (a *Application) Run(ctx context.Context) error {
	if a.mqttClient != nil {
		defer a.mqttClient.Disconnect(250)
	}

	go profiler.Start(a.cfg.Profilling, a.logger)

	a.workerPool = worker.NewPool(15, a.cfg, a.logger, a.mqttClient, a.plcSvc)
	a.workerPool.Start()
	defer a.workerPool.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.Println("Shutdown signal received")
			return nil
		default:
			// delegate all reads to plcservice
			a.plcSvc.ReadAndEnqueue(ctx, a.workerPool)
		}
	}
}
