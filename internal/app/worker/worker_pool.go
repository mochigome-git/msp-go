package worker

import (
	"log"
	"sync"

	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/mqtt"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	jsoniter "github.com/json-iterator/go"
)

type Pool struct {
	workers    int
	dataCh     chan map[string]interface{}
	wg         sync.WaitGroup
	mqttClient MQTT.Client
	logger     *log.Logger
	cfg        config.AppConfig
}

func NewPool(workers int, cfg config.AppConfig, logger *log.Logger, mqttClient MQTT.Client) *Pool {
	return &Pool{
		workers:    workers,
		dataCh:     make(chan map[string]interface{}),
		cfg:        cfg,
		logger:     logger,
		mqttClient: mqttClient,
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
		payload, err := jsoniter.Marshal(msg)
		if err != nil {
			p.logger.Printf("JSON marshal error: %v", err)
			continue
		}
		topic := p.cfg.MqttTopic + msg["address"].(string)
		mqtt.PublishMessage(p.mqttClient, topic, string(payload), p.logger)
	}
}
