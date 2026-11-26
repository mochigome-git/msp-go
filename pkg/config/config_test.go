package config

import (
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

func TestLoad_SecondaryFxModel(t *testing.T) {
	t.Setenv("MQTT_SKIP", "true")
	t.Setenv("PLC_MODEL", "true")
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if !Cfg.PLCs[1].FxModel {
		t.Error("SEC_PLC_MODEL unset should fall back to PLC_MODEL")
	}

	t.Setenv("SEC_PLC_MODEL", "false")
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if !Cfg.PLCs[0].FxModel || Cfg.PLCs[1].FxModel {
		t.Errorf("main fx=%v secondary fx=%v, want true/false", Cfg.PLCs[0].FxModel, Cfg.PLCs[1].FxModel)
	}
}

func TestLoad_InvalidValuesAreErrors(t *testing.T) {
	t.Setenv("MQTT_SKIP", "true")
	t.Setenv("PLC_PORT", "50x1")
	t.Setenv("BLOCK_READ", "yes please")
	err := Load()
	if err == nil || !strings.Contains(err.Error(), "PLC_PORT") || !strings.Contains(err.Error(), "BLOCK_READ") {
		t.Fatalf("want errors for PLC_PORT and BLOCK_READ, got %v", err)
	}
}

func TestLoad_MqttHostRequired(t *testing.T) {
	t.Setenv("MQTT_SKIP", "false")
	t.Setenv("MQTT_HOST", "")
	if err := Load(); err == nil {
		t.Fatal("want error for empty MQTT_HOST")
	}
}

func TestLoad_PprofDisabledByDefault(t *testing.T) {
	t.Setenv("MQTT_SKIP", "true")
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if Cfg.PprofPort != 0 {
		t.Errorf("pprof port = %d, want 0", Cfg.PprofPort)
	}
	t.Setenv("PPROFT_PORT", "6061") // legacy name still honoured
	_ = Load()
	if Cfg.PprofPort != 6061 {
		t.Errorf("legacy PPROFT_PORT: got %d", Cfg.PprofPort)
	}
}

// The shipped example must load without errors.
func TestLoad_ExampleFile(t *testing.T) {
	env, err := godotenv.Read("../../.env.local.example")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	if err := Load(); err != nil {
		t.Fatalf("example config invalid: %v", err)
	}
	if Cfg.PLCs[0].Host == "" || Cfg.PLCs[0].Port != 5012 {
		t.Errorf("main PLC = %+v", Cfg.PLCs[0])
	}
}
