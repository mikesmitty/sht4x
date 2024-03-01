package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/mikesmitty/sht4x"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/host/v3"
)

func main() {
	bus := flag.String("bus", "", "Name of the bus")
	flag.Parse()

	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}

	b, err := i2creg.Open(*bus)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to open I²C bus: %w", err))
	}
	defer b.Close()

	dev, err := sht4x.New(b, nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Serial: %d\n", dev.Serial)

	var e physic.Env
	err = dev.Sense(&e)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Temperature: %0.2f\nHumidity: %s\n", e.Temperature.Celsius(), e.Humidity)

	e, err = dev.ActivateHeater(sht4x.HeaterHighLong)
	if err != nil {
		log.Fatal(err)
	}

	ch, err := dev.SenseContinuous(1 * time.Second)
	if err != nil {
		log.Fatal(err)
	}
	for e := range ch {
		fmt.Printf("Temperature: %0.2f\nHumidity: %s\n", e.Temperature.Celsius(), e.Humidity)
	}
}
