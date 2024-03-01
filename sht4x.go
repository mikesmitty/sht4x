package sht4x

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"periph.io/x/conn/v3"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/physic"
)

type Opts struct {
	// Address is the I2C address of the sensor
	I2cAddress uint16
	Name       string
}

func DefaultOpts() *Opts {
	return &Opts{
		I2cAddress: 0x44,
		Name:       "sht4x",
	}
}

// New opens a handle to an SHT4x sensor at a specified I2C address.
func New(b i2c.Bus, opts *Opts) (*Dev, error) {
	if opts == nil {
		opts = DefaultOpts()
	}
	d := &Dev{
		c:         i2c.Dev{Bus: b, Addr: opts.I2cAddress},
		Name:      opts.Name,
		measDelay: 10 * time.Millisecond,
	}

	// Soft reset the sensor to ensure it's in a known state
	err := d.Reset()
	if err != nil {
		return nil, err
	}

	d.Serial, err = d.GetSerial()
	if err != nil {
		return nil, err
	}

	return d, nil
}

type Dev struct {
	c         i2c.Dev
	measDelay time.Duration
	Name      string
	Serial    uint32

	mu   sync.Mutex
	stop chan struct{}
	wg   sync.WaitGroup
}

func (d *Dev) Sense(e *physic.Env) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		return d.wrap(errors.New("already sensing continuously"))
	}

	return d.sense(e)
}

// SenseContinuous returns measurements as °C on a continuous basis.
//
// The application must call Halt() to stop the sensing when done to stop the
// sensor and close the channel.
//
// It's the responsibility of the caller to retrieve the values from the
// channel as fast as possible, otherwise the interval may not be respected.
func (d *Dev) SenseContinuous(interval time.Duration) (<-chan physic.Env, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		// Don't send the stop command to the device.
		close(d.stop)
		d.stop = nil
		d.wg.Wait()
	}

	sensing := make(chan physic.Env)
	d.stop = make(chan struct{})
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer close(sensing)
		d.sensingContinuous(interval, sensing, d.stop)
	}()
	return sensing, nil
}

func (d *Dev) String() string {
	return fmt.Sprintf("%s{%d}", d.Name, d.Serial)
}

// 15-Bit ADC Resolution; Nominal temperature resolution varies due to RTD non-linearity
func (d *Dev) Precision(e *physic.Env) {
	e.Temperature = physic.Kelvin / 32
}

// Halt stops the SHT4x from acquiring measurements as initiated by
// SenseContinuous().
//
// It is recommended to call this function before terminating the process to
// reduce idle power usage and a goroutine leak.
func (d *Dev) Halt() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop == nil {
		return nil
	}
	close(d.stop)
	d.stop = nil
	d.wg.Wait()

	return nil
}

func (d *Dev) sense(e *physic.Env) error {
	// Measure T & RH with high precision (high repeatability)
	if err := d.c.Tx([]byte{commandMeasureHighPrecision}, nil); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)

	return d.parseTemperature(e)
}

func (d *Dev) parseTemperature(e *physic.Env) error {
	var data [6]byte
	if err := d.c.Tx(nil, data[:]); err != nil {
		return err
	}

	tTicks := readUint(data[0], data[1])
	if err := verifyChecksum(data[:3]); err != nil {
		return err
	}
	rhTicks := readUint(data[3], data[4])
	if err := verifyChecksum(data[3:]); err != nil {
		return err
	}

	// Convert ticks to physical values
	temp := (-45 + (175 * float64(tTicks) / 65535))
	rh := (-6 + (125 * float64(rhTicks) / 65535))

	// Datasheet page 13
	if rh < 0 {
		rh = 0
	} else if rh > 100 {
		rh = 100
	}

	e.Temperature = physic.Temperature(temp*1000)*physic.MilliCelsius + physic.ZeroCelsius
	e.Humidity = physic.RelativeHumidity(rh*10000) * physic.MicroRH

	return nil
}

func (d *Dev) sensingContinuous(interval time.Duration, sensing chan<- physic.Env, stop <-chan struct{}) {
	// Ensure the interval is at least the minimum measurement delay.
	if interval < d.measDelay {
		interval = d.measDelay
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	var err error
	for {
		// Do one initial sensing right away.
		e := physic.Env{}
		d.mu.Lock()
		err = d.sense(&e)
		d.mu.Unlock()
		if err != nil {
			fmt.Printf("sensingContinuous: %s\n", err) // FIXME
			return
		}
		select {
		case sensing <- e:
		case <-stop:
			return
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

func (d *Dev) GetSerial() (uint32, error) {
	if err := d.c.Tx([]byte{commandReadSerialNumber}, nil); err != nil {
		return 0, err
	}
	time.Sleep(1 * time.Millisecond)
	var data [6]byte
	if err := d.c.Tx(nil, data[:]); err != nil {
		return 0, err
	}
	if err := verifyChecksum(data[:3]); err != nil {
		return 0, err
	}
	if err := verifyChecksum(data[3:]); err != nil {
		return 0, err
	}
	serial := uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[3])<<8 | uint32(data[4])

	return serial, nil
}

// ActivateHeater activates the heater for a specified duration. Not intended to be used
// at greater than a 10% duty cycle for the life of the sensor.
func (d *Dev) ActivateHeater(mode int) (physic.Env, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	interval := 110 * time.Millisecond
	long := 1100 * time.Millisecond

	var cmd byte
	var e physic.Env
	switch mode {
	case HeaterLow:
		cmd = commandHeaterLow100ms
	case HeaterLowLong:
		cmd = commandHeaterLow1s
		interval = long
	case HeaterMedium:
		cmd = commandHeaterMedium100ms
	case HeaterMediumLong:
		cmd = commandHeaterMedium1s
		interval = long
	case HeaterHigh:
		cmd = commandHeaterHigh100ms
	case HeaterHighLong:
		cmd = commandHeaterHigh1s
		interval = long
	default:
		return e, errors.New("sht4x: invalid heater mode")
	}

	if err := d.c.Tx([]byte{cmd}, nil); err != nil {
		return e, err
	}

	time.Sleep(interval)

	err := d.parseTemperature(&e)
	return e, err
}

func (d *Dev) Reset() error {
	err := d.c.Tx([]byte{commandSoftReset}, nil)
	time.Sleep(1 * time.Millisecond)
	return err
}

func (d *Dev) wrap(err error) error {
	return fmt.Errorf("%s: %v", strings.ToLower(d.Name), err)
}

func readUint(msb, lsb byte) uint16 {
	return uint16(msb)<<8 | uint16(lsb)
}

func verifyChecksum(data []byte) error {
	if len(data) != 3 {
		return errors.New("sht4x: invalid data length")
	}
	if data[2] != checksum(data[:2]) {
		return errors.New("sht4x: invalid checksum")
	}
	return nil
}

func checksum(data []byte) byte {
	var crc uint8 = 0xFF
	for i := 0; i < len(data); i++ {
		crc ^= data[i]
		for j := 0; j < 8; j++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x31
			} else {
				crc = crc << 1
			}
		}
	}
	return crc
}

var _ conn.Resource = &Dev{}
var _ physic.SenseEnv = &Dev{}
