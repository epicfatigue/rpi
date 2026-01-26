//go:build linux
package hal

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// Run:
//   sudo env "PATH=$PATH" RPI_HAL_HW_TEST=1 RPI_HAL_TEST_GPIO=22 go test -run TestHardwareDigitalPinBlink -v ./hal
//
// Stop reef-pi first if it might be holding the GPIO:
//   sudo pkill reef-pi || true
func TestHardwareDigitalPinBlink(t *testing.T) {
	if os.Getenv("RPI_HAL_HW_TEST") != "1" {
		t.Skip("hardware test disabled. Set RPI_HAL_HW_TEST=1 to run")
	}

	pin := 22
	if s := os.Getenv("RPI_HAL_TEST_GPIO"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("invalid RPI_HAL_TEST_GPIO=%q: %v", s, err)
		}
		pin = v
	}

	dp, err := newDigitalPin(pin)
	if err != nil {
		t.Fatalf("newDigitalPin(%d) failed: %v (is another process holding the line?)", pin, err)
	}
	defer func() { _ = dp.Close() }()

	if err := dp.SetDirection(true); err != nil {
		t.Fatalf("SetDirection(output) failed for GPIO%d: %v", pin, err)
	}

	t.Logf("Blinking GPIO%d: HIGH 1s, LOW 1s, repeated 5 times", pin)
	for i := 0; i < 5; i++ {
		if err := dp.Write(1); err != nil {
			t.Fatalf("Write(HIGH) failed on iteration %d: %v", i, err)
		}
		time.Sleep(1 * time.Second)

		if err := dp.Write(0); err != nil {
			t.Fatalf("Write(LOW) failed on iteration %d: %v", i, err)
		}
		time.Sleep(1 * time.Second)
	}
}
