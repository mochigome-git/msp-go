package mitsubishi

import (
	"fmt"

	"github.com/mochigome-git/msp-go/pkg/mcp"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// ValidateReadDevice checks a DEVICES_* entry before any request is sent:
// the device code must be one the frame builder knows (an unknown code
// would produce a malformed request), the number must parse in the
// device's base, and the third field must be a known data-type code.
func ValidateReadDevice(d PLC_Utils.Device, fx bool) error {
	if err := validateAddress(d, fx); err != nil {
		return err
	}
	if d.NumberRegisters < 1 || d.NumberRegisters > 7 {
		return fmt.Errorf("%s%s: data type %d is not one of 1-7", d.DeviceType, d.DeviceNumber, d.NumberRegisters)
	}
	return nil
}

// ValidateWriteDevice checks a WRITE_MAP_* target.
func ValidateWriteDevice(d PLC_Utils.Device, fx bool) error {
	if err := validateAddress(d, fx); err != nil {
		return err
	}
	if d.ProcessNumber < 1 || d.ProcessNumber > 6 {
		return fmt.Errorf("%s%s: write type %d is not one of 1-6", d.DeviceType, d.DeviceNumber, d.ProcessNumber)
	}
	if d.NumberRegisters < 1 {
		return fmt.Errorf("%s%s: registers per write must be >= 1", d.DeviceType, d.DeviceNumber)
	}
	return nil
}

func validateAddress(d PLC_Utils.Device, fx bool) error {
	known := false
	if fx {
		_, known = mcp.DeviceCodeFx(d.DeviceType)
	} else {
		_, known = mcp.DeviceCode(d.DeviceType)
	}
	if !known {
		return fmt.Errorf("device type %q is not supported (fx=%v)", d.DeviceType, fx)
	}
	n, err := parseDeviceNumber(d.DeviceType, d.DeviceNumber)
	if err != nil || n < 0 {
		return fmt.Errorf("%s%s: invalid device number", d.DeviceType, d.DeviceNumber)
	}
	return nil
}
