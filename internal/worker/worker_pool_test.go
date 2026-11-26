package worker

import (
	"testing"
	"time"

	"github.com/mochigome-git/msp-go/internal/plcservice"
	"github.com/mochigome-git/msp-go/pkg/config"
)

func TestTopic(t *testing.T) {
	cfg := config.AppConfig{MqttTopic: "plant/"}
	if got := Topic(cfg, "main", "D100"); got != "plant/D100" {
		t.Errorf("default topic = %s", got)
	}
	cfg.MqttTopicIncludeSource = true
	if got := Topic(cfg, "secondary", "D100"); got != "plant/secondary/D100" {
		t.Errorf("topic with source = %s", got)
	}
}

func TestChanged(t *testing.T) {
	cfg := config.AppConfig{PublishOnChange: true, PublishRefresh: 50 * time.Millisecond}
	p := NewPool(1, cfg, nil, nil, nil, plcservice.WriteMapWithCond{})
	msg := func(src string, v any) map[string]any {
		return map[string]any{"address": "D1", "value": v, "source": src}
	}

	steps := []struct {
		m    map[string]any
		want bool
	}{
		{msg("main", 1), true},      // first value
		{msg("main", 1), false},     // unchanged
		{msg("secondary", 1), true}, // same address, other PLC
		{msg("main", 2), true},      // changed
		{msg("main", 2), false},     // unchanged
	}
	for i, s := range steps {
		if got := p.changed(s.m); got != s.want {
			t.Fatalf("step %d: changed = %v, want %v", i, got, s.want)
		}
	}
	time.Sleep(60 * time.Millisecond)
	if !p.changed(msg("main", 2)) {
		t.Fatal("unchanged value not re-sent after refresh interval")
	}
}
