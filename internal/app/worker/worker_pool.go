package worker

import (
	"fmt"
	"log"
	"sync"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	jsoniter "github.com/json-iterator/go"

	"msp-go/internal/app"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"
)

// Pool manages concurrent workers that either publish MQTT or write directly to PLC
type Pool struct {
	workers    int
	dataCh     chan map[string]interface{}
	wg         sync.WaitGroup
	mqttClient MQTT.Client
	logger     *log.Logger
	cfg        config.AppConfig
	app        *app.Application 
}

// NewPool creates a new worker pool
func NewPool(workers int, cfg config.AppConfig, logger *log.Logger, mqttClient MQTT.Client, appRef *app.Application) *Pool {
	return &Pool{
		workers:    workers,
		dataCh:     make(chan map[string]interface{}),
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
		app:        appRef, 
	}
}

func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.workerRoutine()
	}
}

func (p *Pool) Stop() {
	close(p.dataCh)
	p.wg.Wait()
}

func (p *Pool) Enqueue(msg map[string]interface{}) {
	p.dataCh <- msg
}

func (p *Pool) workerRoutine() {
	defer p.wg.Done()
	for msg := range p.dataCh {
		// if MQTT_SKIP=true → Write directly to PLC
		if p.cfg.MqttSkip {
			if err := p.directWriteToPLC(msg); err != nil {
				p.logger.Printf("Direct PLC write failed: %v", err)
			}
			continue
		}

		// Otherwise publish via MQTT
		payload, err := jsoniter.Marshal(msg)
		if err != nil {
			p.logger.Printf("JSON marshal error: %v", err)
			continue
		}
		topic := p.cfg.MqttTopic + msg["address"].(string)
		mqtt.PublishMessage(p.mqttClient, topic, string(payload), p.logger)
	}
}


//  direct PLC write using Application.WritePLC()
func (p *Pool) directWriteToPLC(msg map[string]interface{}) error {
	if p.app == nil {
		return fmt.Errorf("PLC Application reference is nil")
	}

	fmt.Println(msg)

	/*
	// Example: your config string like "D,100,1,1,Y,10,1,1,X,20,1,1"
	devicesStr := strings.Split(p.cfg.PlcDeviceUpsert, ",")
	if len(devicesStr)%4 != 0 {
		return fmt.Errorf("invalid PLC device config format: must be multiple of 4 items")
	}

	dataList := []any{
		msg["YStatus"],
		msg["XStatus"],
		msg["VacuumStatus"],
	}

	deviceCount := len(devicesStr) / 4
	if deviceCount != len(dataList) {
		return fmt.Errorf("PLC device/data count mismatch: %d devices vs %d data items",
			deviceCount, len(dataList))
	}

	for i := 0; i < deviceCount; i++ {
		deviceStr := strings.Join(devicesStr[i*4:i*4+4], ",")
		value := dataList[i]

		p.logger.Printf("Writing PLC: %s -> %v", deviceStr, value)
		if err := p.app.WritePLC(context.Background(), deviceStr, value); err != nil {
			return fmt.Errorf("PLC write failed for %s: %w", deviceStr, err)
		}
		p.logger.Printf("PLC write success: %s", deviceStr)
	}*/

	return nil
}
