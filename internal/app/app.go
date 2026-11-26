package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/mochigome-git/msp-go/internal/plcservice"
	"github.com/mochigome-git/msp-go/internal/profiler"
	"github.com/mochigome-git/msp-go/internal/worker"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"
)

const workers = 15

// Application orchestrates MQTT, worker pool, and PLC service
type Application struct {
	cfg        config.AppConfig
	logger     *log.Logger
	mqttClient MQTT.Client
	workerPool *worker.Pool
	writeMap   plcservice.WriteMapWithCond

	plcSvc *plcservice.Service
}

// NewApplication validates the configuration, then initializes the PLC
// service and MQTT. Any invalid device or write-map entry is an error, so
// a typo stops startup instead of reading or writing the wrong address.
func NewApplication(cfg config.AppConfig, logger *log.Logger) (*Application, error) {
	plcSvc := plcservice.NewService(logger)
	plcSvc.SetPollInterval(cfg.PollInterval)
	for _, plcCfg := range cfg.PLCs {
		devices := []string{
			plcCfg.Devices2,
			plcCfg.Devices16,
			plcCfg.Devices32,
			plcCfg.DevicesAscii,
		}
		if err := plcSvc.InitPLC(plcCfg, devices); err != nil {
			plcSvc.Close()
			return nil, err
		}
	}
	if !plcSvc.HasDevices() {
		plcSvc.Close()
		return nil, errors.New("no PLC has devices to read (check PLC_HOST and DEVICES_*)")
	}

	var writeMap plcservice.WriteMapWithCond
	if cfg.MqttSkip {
		wm, err := plcservice.ParseWriteMap(cfg)
		if err == nil {
			err = plcSvc.ValidateWriteMap(cfg, wm)
		}
		if err != nil {
			plcSvc.Close()
			return nil, fmt.Errorf("invalid write map:\n%w", err)
		}
		writeMap = wm
	} else {
		for _, p := range cfg.PLCs {
			if strings.TrimSpace(p.WriteMap) != "" || strings.TrimSpace(p.CondMap) != "" {
				logger.Printf("⚠️ PLC %s: WRITE_MAP_* is set but ignored in MQTT mode (set MQTT_SKIP=true for direct writes)", p.Name)
			}
		}
	}

	var mqttClient MQTT.Client
	if !cfg.MqttSkip {
		c, err := mqtt.NewClient(cfg, logger)
		if err != nil {
			plcSvc.Close()
			return nil, err
		}
		mqttClient = c
		logger.Println("MQTT client initialized")
	} else {
		logger.Println("⚠️ MQTT initialization skipped (MQTT_SKIP=true)")
	}

	return &Application{
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
		writeMap:   writeMap,
		plcSvc:     plcSvc,
	}, nil
}

// Run starts profiler, worker pool, and the PLC read loop
func (a *Application) Run(ctx context.Context) error {
	// Deferred calls run in reverse: drain the workers first, then
	// disconnect MQTT, then close the PLC connections.
	defer a.plcSvc.Close()
	if a.mqttClient != nil {
		defer a.mqttClient.Disconnect(250)
	}

	go profiler.Start(a.cfg.PprofPort, a.logger)

	a.workerPool = worker.NewPool(workers, a.cfg, a.logger, a.mqttClient, a.plcSvc, a.writeMap)
	a.workerPool.Start()
	defer a.workerPool.Stop()

	if a.cfg.PollInterval > 0 {
		a.logger.Printf("Polling every %s", a.cfg.PollInterval)
	}

	for {
		start := time.Now()
		a.plcSvc.ReadAndEnqueue(ctx, a.workerPool)

		wait := a.cfg.PollInterval - time.Since(start)
		if wait < 0 {
			wait = 0
		}
		select {
		case <-ctx.Done():
			a.logger.Println("Shutdown signal received")
			return nil
		case <-time.After(wait):
		}
	}
}
