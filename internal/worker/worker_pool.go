package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	jsoniter "github.com/json-iterator/go"
	"github.com/mochigome-git/msp-go/internal/plcservice"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"
)

// writeTimeout bounds the PLC writes triggered by one message.
const writeTimeout = 30 * time.Second

// Pool manages concurrent workers that either publish MQTT or write directly to PLC
type Pool struct {
	workers    int
	dataCh     chan map[string]any
	wg         sync.WaitGroup
	mqttClient MQTT.Client
	logger     *log.Logger
	cfg        config.AppConfig
	plcSvc     *plcservice.Service
	writeMap   plcservice.WriteMapWithCond

	// publish-on-change state; only touched by Enqueue (the read loop)
	last map[string]lastPublished
}

type lastPublished struct {
	value string
	at    time.Time
}

// NewPool creates a new worker pool. writeMap is used in direct mode
// (MQTT_SKIP=true) and ignored otherwise.
func NewPool(workers int, cfg config.AppConfig, logger *log.Logger, mqttClient MQTT.Client, plcSvc *plcservice.Service, writeMap plcservice.WriteMapWithCond) *Pool {
	return &Pool{
		workers:    workers,
		dataCh:     make(chan map[string]any),
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
		plcSvc:     plcSvc,
		writeMap:   writeMap,
		last:       make(map[string]lastPublished),
	}
}

func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.workerRoutine()
	}
}

// Stop waits for queued messages to be handled. Enqueue must not be
// called after Stop.
func (p *Pool) Stop() {
	close(p.dataCh)
	p.wg.Wait()
}

// Enqueue hands a read value to the workers. With PUBLISH_ON_CHANGE in
// MQTT mode, unchanged values are dropped here except for a periodic
// refresh, so subscribers that start late still receive every value.
// Enqueue is called from the single read loop and is not safe for
// concurrent use.
func (p *Pool) Enqueue(msg map[string]any) {
	if !p.cfg.MqttSkip && p.cfg.PublishOnChange && !p.changed(msg) {
		return
	}
	p.dataCh <- msg
}

func (p *Pool) changed(msg map[string]any) bool {
	key := fmt.Sprint(msg["source"], "|", msg["address"])
	val := fmt.Sprint(msg["value"])
	now := time.Now()
	prev, ok := p.last[key]
	if ok && prev.value == val && (p.cfg.PublishRefresh <= 0 || now.Sub(prev.at) < p.cfg.PublishRefresh) {
		return false
	}
	p.last[key] = lastPublished{value: val, at: now}
	return true
}

// Topic returns the MQTT topic for a message.
func Topic(cfg config.AppConfig, source, address string) string {
	if cfg.MqttTopicIncludeSource && source != "" {
		return cfg.MqttTopic + source + "/" + address
	}
	return cfg.MqttTopic + address
}

func (p *Pool) workerRoutine() {
	defer p.wg.Done()

	for msg := range p.dataCh {
		if p.cfg.MqttSkip {
			// Direct PLC write mode only
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			if err := p.plcSvc.DirectWrite(ctx, msg, p.writeMap); err != nil {
				p.logger.Printf("Direct PLC write failed: %v", err)
			}
			cancel()
			continue
		}

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
		source, _ := msg["source"].(string)
		mqtt.PublishMessage(p.mqttClient, Topic(p.cfg, source, address), string(payload), p.logger)
	}
}
