package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mikesmitty/sht4x"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/host/v3"
)

/*
This tool is used to decontaminate a sensor by activating the heater for an extended period of time to offgas VOC contaminants,
based on information provided by Sensirion below. Use with caution and at your own risk.
https://web.archive.org/web/20221006045126/https://sensirion.com/media/documents/FEE9F039/62459F54/Application_Note_Heater_Decontamination_SHT4xX.pdf
*/
func main() {
	bus := flag.String("bus", "", "Name of the bus")
	ack := flag.Bool("acknowledge-fire-warning", false, "Acknowledge warning regarding fire hazard and/or potential hardware failure")
	dur := flag.Duration("duration", 0, "Duration to activate heater for")
	flag.Parse()

	if !*ack {
		fmt.Println("WARNING: This example activates the heater of the sensor for extended periods of time. This can cause the sensor to reach boiling hot temperatures of up to 110°C. This can cause house fires, burns or other injuries as well as subjects the sensor to potential hardware failure. Do not touch the sensor while the heater is activated.")
		fmt.Println("By running this command you agree to waive all claims of liability for any damage this tool may cause. To acknowledge this warning, please run the example with the -acknowledge-fire-warning flag.")
		os.Exit(1)
	}

	if _, err := host.Init(); err != nil {
		fatal("host init failed", err, 2)
	}

	b, err := i2creg.Open(*bus)
	if err != nil {
		fatal("failed to open I²C", err, 2)
	}
	defer b.Close()

	dev, err := sht4x.New(b, nil)
	if err != nil {
		fatal("sensor error", err, 2)
	}

	slog.SetDefault(slog.Default().With("serial", dev.Serial))

	var e physic.Env
	err = dev.Sense(&e)
	if err != nil {
		fatal("sensor read failed", err, 2)
	}

	t := time.NewTimer(*dur)
	tk := time.NewTicker(1 * time.Minute)
	lowTemp := e.Temperature.Celsius()

	slog.Info("beginning heat cycle", "duration", *dur, "temperature", e.Temperature.Celsius())

	for {
		select {
		case <-t.C:
			slog.Info("heat cycle completed after", "duration", *dur)
			return
		case <-tk.C:
			slog.Info("status", "temperature", e.Temperature.Celsius())
		default:
			e, err = heatCycle(dev, lowTemp)
			if err != nil {
				fatal("heat cycle failed", err, 2)
			}
		}
	}
}

func heatCycle(dev *sht4x.Dev, lowTemp float64) (physic.Env, error) {
	e, err := dev.ActivateHeater(sht4x.HeaterHighLong)
	if err != nil {
		return e, fmt.Errorf("heater activation failed: %w", err)
	}

	if e.Temperature.Celsius() < lowTemp {
		return e, fmt.Errorf("temperature did not increase after activating heater")
	}
	if e.Temperature.Celsius() > 110 {
		slog.Warn("temperature is above 110°C. Pausing for 10 seconds to allow sensor to cool down.", "temperature", e.Temperature.Celsius())
		time.Sleep(10 * time.Second)
		for {
			err = dev.Sense(&e)
			if err != nil {
				return e, err
			}
			if e.Temperature.Celsius() < 110 {
				break
			}
		}
	}
	return e, nil
}

func fatal(msg string, err error, code int) {
	slog.Error(msg, "error", err)
	os.Exit(code)
}
