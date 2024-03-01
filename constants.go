package sht4x

const (
	// Activate heater at 200mw
	commandHeaterHigh1s    = 0x39
	commandHeaterHigh100ms = 0x32
	// Activate heater at 110mw
	commandHeaterMedium1s    = 0x2F
	commandHeaterMedium100ms = 0x24
	// Activate heater at 20mw
	commandHeaterLow1s    = 0x1E
	commandHeaterLow100ms = 0x15

	commandMeasureHighPrecision   = 0xFD
	commandMeasureMediumPrecision = 0xF6
	commandMeasureLowPrecision    = 0xE0
	commandReadSerialNumber       = 0x89
	commandSoftReset              = 0x94
)

const (
	HeaterLow = iota
	HeaterLowLong
	HeaterMedium
	HeaterMediumLong
	HeaterHigh
	HeaterHighLong
)
