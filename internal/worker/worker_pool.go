package worker

import (
	"context"
	"log"
	"sync"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	jsoniter "github.com/json-iterator/go"
	"github.com/mochigome-git/msp-go/internal/plcservice"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"
)

// Pool manages concurrent workers that either publish MQTT or write directly to PLC
type Pool struct {
	workers    int
	dataCh     chan map[string]any
	wg         sync.WaitGroup
	mqttClient MQTT.Client
	logger     *log.Logger
	cfg        config.AppConfig
	plcSvc     *plcservice.Service
}

// NewPool creates a new worker pool
func NewPool(workers int, cfg config.AppConfig, logger *log.Logger, mqttClient MQTT.Client, plcSvc *plcservice.Service) *Pool {
	return &Pool{
		workers:    workers,
		dataCh:     make(chan map[string]any),
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
		plcSvc:     plcSvc,
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

func (p *Pool) Enqueue(msg map[string]any) {
	p.dataCh <- msg
}

func (p *Pool) workerRoutine() {
	defer p.wg.Done()

	// Build write map once
	writeMap := plcservice.BuildWriteMap(p.cfg)

	for msg := range p.dataCh {
		ctx := context.Background()

		if p.cfg.MqttSkip {
			// Direct PLC write mode only
			if err := p.plcSvc.DirectWrite(ctx, msg, writeMap); err != nil {
				p.logger.Printf("Direct PLC write failed: %v", err)
			}
		} else {
			// MQTT mode only
			payload, err := jsoniter.Marshal(msg)
			if err != nil {
				p.logger.Printf("JSON marshal error: %v", err)
				continue
			}

			address, ok := msg["address"].(string)
			if !ok {
				p.logger.Printf("Missing address in message, skipping")
				continue
			}

			topic := p.cfg.MqttTopic + address
			mqtt.PublishMessage(p.mqttClient, topic, string(payload), p.logger)
		}
	}
}
