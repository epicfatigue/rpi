package hal

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/reef-pi/hal"
	"github.com/reef-pi/rpi/pwm"
)

type PinFactory func(key interface{}) (DigitalPin, error)

type rpiFactory struct {
	meta       hal.Metadata
	parameters []hal.ConfigParameter
}

type pinFactory func(int) (DigitalPin, error)

var rFactory *rpiFactory
var once sync.Once

// RpiFactory provides the factory to get RPI Driver parameters and RPI Drivers
func RpiFactory() hal.DriverFactory {
	once.Do(func() {
		rFactory = &rpiFactory{
			meta: hal.Metadata{
				Name:         "rpi",
				Description:  "hardware peripherals and GPIO channels on the base raspberry pi hardware",
				Capabilities: []hal.Capability{hal.DigitalInput, hal.DigitalOutput, hal.PWM},
			},
			parameters: []hal.ConfigParameter{
				{
					Name:    "Frequency",
					Type:    hal.Integer,
					Order:   0,
					Default: "200",
				},
				{
					Name:    "Dev Mode",
					Type:    hal.Boolean,
					Order:   1,
					Default: false,
				},
			},
		}
	})
	return rFactory
}

func (f *rpiFactory) GetParameters() []hal.ConfigParameter {
	return f.parameters
}

func (f *rpiFactory) ValidateParameters(parameters map[string]interface{}) (bool, map[string][]string) {
	var failures = make(map[string][]string)

	var v interface{}
	var ok bool

	if v, ok = parameters["Frequency"]; ok {
		_, ok := hal.ConvertToInt(v)
		if !ok {
			failure := fmt.Sprint("Frequency is not a number. ", v, " was received.")
			failures["Frequency"] = append(failures["Frequency"], failure)
		}
	} else {
		failure := fmt.Sprint("Frequency is required parameter, but was not received.")
		failures["Frequency"] = append(failures["Frequency"], failure)
	}

	if v, ok = parameters["Dev Mode"]; ok {
		_, ok := v.(bool)
		if !ok {
			failure := fmt.Sprint("Dev Mode is not a boolean. ", v, " was received.")
			failures["Dev Mode"] = append(failures["Dev Mode"], failure)
		}
	} else {
		failure := fmt.Sprint("Dev Mode is required parameter, but was not received.")
		failures["Dev Mode"] = append(failures["Dev Mode"], failure)
	}

	return len(failures) == 0, failures
}

func (f *rpiFactory) Metadata() hal.Metadata {
	return f.meta
}

func (f *rpiFactory) NewDriver(parameters map[string]interface{}, _ interface{}) (hal.Driver, error) {
	if valid, failures := f.ValidateParameters(parameters); !valid {
		return nil, errors.New(hal.ToErrorString(failures))
	}

	devMode := parameters["Dev Mode"].(bool)
	frequency, _ := hal.ConvertToInt(parameters["Frequency"])

	// ---------------------------------------------------------------------
	// FIX: On Pi OS Bookworm / Pi 5, /dev/gpiomem may not exist, but GPIO
	// is available via the character device interface (/dev/gpiochip0).
	//
	// If the user has Dev Mode enabled but gpiochip0 exists, automatically
	// override to REAL mode so GPIO works.
	// ---------------------------------------------------------------------
	if devMode {
		if _, err := os.Stat("/dev/gpiochip0"); err == nil {
			log.Println("[rpi] gpiochip0 detected, overriding DEV Mode -> REAL mode")
			devMode = false
		} else {
			log.Printf("[rpi] DEV Mode requested and gpiochip0 not available: %v", err)
		}
	}

	var pwmDriver pwm.Driver
	var pf pinFactory

	if devMode {
		log.Println("RPI Driver using DEV Mode")
		pwmDriver, _ = pwm.Noop()
		pf = NoopPinFactory
	} else {
		log.Println("[rpi] RPI Driver using REAL GPIO mode")
		pwmDriver = pwm.New()
		pf = newDigitalPin
	}

	return newDriver(pwmDriver, pf, f.meta, frequency)
}

func newDriver(pd pwm.Driver, factory pinFactory, meta hal.Metadata, frequency int) (hal.Driver, error) {
	d := &driver{
		pins:     make(map[int]*pin),
		channels: make(map[int]*channel),
		meta:     meta,
	}

	// IMPORTANT CHANGE:
	// Don't fail the entire driver if a pin is busy (e.g., GPIO4 used by 1-Wire).
	// Skip unavailable pins and keep going.
	for i := range validGPIOPins {
		p, err := factory(i)
		if err != nil {
			log.Printf("[rpi] skipping GPIO %d: %v", i, err)
			continue
		}

		name := fmt.Sprintf("GP%d", i)
		d.pins[i] = &pin{
			name:       name,
			number:     i,
			digitalPin: p,
		}
	}

	for _, p := range []int{0, 1} {
		ch := &channel{
			pin:       p,
			driver:    pd,
			frequency: frequency,
			name:      fmt.Sprintf("%d", p),
		}
		d.channels[p] = ch
	}

	return d, nil
}
