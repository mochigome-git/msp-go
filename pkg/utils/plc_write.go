package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/plc_interface"
)

func DirectWriteSrcToDest(
	ctx context.Context,
	msg map[string]interface{},
	cfg config.PlcConfig,
	logger func(format string, v ...any),
	plcWriter plc_interface.PLCWriter,
) error {
	mapStr := cfg.WriteMapSrctoDest
	if mapStr == "" {
		logger("⚠️ No WRITE_MAP_SRC_TO_DEST_16bit defined — skipping mapping")
		return nil
	}

	address, ok := msg["address"].(string)
	if !ok {
		return fmt.Errorf("invalid message: missing 'address'")
	}
	valueStr := fmt.Sprintf("%v", msg["value"])

	// Split mapping per pair
	maps := strings.Split(mapStr, ";")
	for _, m := range maps {
		pair := strings.SplitN(m, ">", 2)
		if len(pair) != 2 {
			logger("⚠️ Invalid mapping pair: %s", m)
			continue
		}

		src := strings.TrimSpace(pair[0])
		dest := strings.TrimSpace(pair[1])

		if src != address {
			continue
		}

		// Write via PLC interface
		if err := plcWriter.WritePLC(ctx, dest, valueStr); err != nil {
			return fmt.Errorf("PLC write failed for %s → %s: %w", src, dest, err)
		}

		logger("✅ %s → %s | value=%v", src, dest, valueStr)
		return nil
	}

	// logger("⚠️ No matching map found for %s", address)
	return nil
}
