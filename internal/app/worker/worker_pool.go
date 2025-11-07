package worker

import (
	"context"
	"log"
	"sync"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	jsoniter "github.com/json-iterator/go"
    "github.com/mochigome-git/msp-go/pkg/utils"
	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"
	"github.com/mochigome-git/msp-go/pkg/plc_interface"
)


// Pool manages concurrent workers that either publish MQTT or write directly to PLC
type Pool struct {
	workers    int
	dataCh     chan map[string]interface{}
	wg         sync.WaitGroup
	mqttClient MQTT.Client
	logger     *log.Logger
	cfg        config.AppConfig
 	plcWriter  plc_interface.PLCWriter
}

// NewPool creates a new worker pool
func NewPool(workers int, cfg config.AppConfig, logger *log.Logger, mqttClient MQTT.Client, plcWriter plc_interface.PLCWriter) *Pool {
	return &Pool{
		workers:    workers,
		dataCh:     make(chan map[string]interface{}),
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
		plcWriter:  plcWriter,
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
			ctx := context.Background()
			if err := utils.DirectWriteSrcToDest(ctx, msg, config.Plc, p.logger.Printf, p.plcWriter); err != nil {
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


