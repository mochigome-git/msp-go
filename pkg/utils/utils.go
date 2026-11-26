package utils

// Device is one PLC address plus how to read or write it.
type Device struct {
	DeviceType      string
	DeviceNumber    string
	ProcessNumber   uint16
	NumberRegisters uint16
}
