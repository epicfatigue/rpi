//go:build linux
package hal

import (
	"log"
	"sync"

	"github.com/warthog618/go-gpiocdev"
)

const rpiGpioChip = "gpiochip0"

type digitalPin struct {
	pin      int
	chip     *gpiocdev.Chip
	line     *gpiocdev.Line
	isOutput bool
	mu       sync.Mutex
}

func newDigitalPin(i int) (DigitalPin, error) {
	chip, err := gpiocdev.NewChip(rpiGpioChip)
	if err != nil {
		return nil, err
	}

	// Request as input initially, but add a consumer label so gpioinfo shows it.
	line, err := chip.RequestLine(i,
		gpiocdev.AsInput,
		gpiocdev.WithConsumer("reef-pi"),
	)
	if err != nil {
		_ = chip.Close()
		return nil, err
	}

	log.Printf("HAL: opened gpiochip=%s line=%d (input)", rpiGpioChip, i)

	return &digitalPin{
		pin:      i,
		chip:     chip,
		line:     line,
		isOutput: false,
	}, nil
}

func (p *digitalPin) SetDirection(output bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var err error
	if output {
		err = p.line.Reconfigure(gpiocdev.AsOutput(0))
	} else {
		err = p.line.Reconfigure(gpiocdev.AsInput)
	}
	if err == nil {
		p.isOutput = output
		log.Printf("HAL: gpio line=%d direction=%s", p.pin, map[bool]string{true: "output", false: "input"}[output])
	} else {
		log.Printf("HAL: gpio line=%d SetDirection error: %v", p.pin, err)
	}
	return err
}

func (p *digitalPin) Read() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, err := p.line.Value()
	if err != nil {
		log.Printf("HAL: gpio line=%d read error: %v", p.pin, err)
		return 0, err
	}
	log.Printf("HAL: gpio line=%d read=%d", p.pin, v)
	return v, nil
}

func (p *digitalPin) Write(value int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reef-pi often calls Write without explicitly calling SetDirection first.
	// Ensure output before setting a value.
	if !p.isOutput {
		if err := p.line.Reconfigure(gpiocdev.AsOutput(0)); err != nil {
			log.Printf("HAL: gpio line=%d auto-set output error: %v", p.pin, err)
			return err
		}
		p.isOutput = true
		log.Printf("HAL: gpio line=%d auto-set direction=output", p.pin)
	}

	log.Printf("HAL: gpio line=%d write=%d", p.pin, value)
	return p.line.SetValue(value)
}

func (p *digitalPin) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("HAL: closing gpio line=%d", p.pin)

	var err1 error
	if p.line != nil {
		err1 = p.line.Close()
		p.line = nil
	}
	if p.chip != nil {
		_ = p.chip.Close()
		p.chip = nil
	}
	return err1
}
