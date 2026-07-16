// Package plc defines the shared interface and utilities for all PLC brands.
// Brand implementations live in sub-packages: pkg/plc/mitsubishi, pkg/plc/shibaura
package plc

import (
	"context"
	"log"
)

// PLCClient is the brand-agnostic interface every PLC driver must satisfy.
// Keep this minimal — only operations that every brand can genuinely support.
type PLCClient interface {
	// ReadData reads numberRegisters worth of data starting at deviceNumber.
	// deviceType: memory area code  (e.g. "D", "M", "W", "R", "X", "Y")
	// deviceNumber: start address   (decimal or hex string depending on brand)
	// fx: Mitsubishi-specific flag; ignored by other brands
	ReadData(
		ctx context.Context,
		deviceType string,
		deviceNumber string,
		numberRegisters uint16,
		fx bool,
	) (any, error)

	// WriteData writes data to the PLC at the given address.
	WriteData(
		deviceType string,
		deviceNumber string,
		writeData []byte,
		numberRegisters uint16,
	) error

	// BatchWrite writes in chunks, handling wrap-around automatically.
	BatchWrite(
		deviceType string,
		startDevice string,
		writeData []byte,
		maxRegistersPerWrite uint16,
		logger *log.Logger,
	) error

	// EncodeData converts a string value to PLC-ready bytes.
	// Each brand implements its own encoding format.
	EncodeData(valueStr string, processNumber int) ([]byte, error)
}
