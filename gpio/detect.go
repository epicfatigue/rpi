//go:build !windows
// +build !windows

package gpio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"syscall"
)

const (
	bcm2835Base = 0x3F000000
	pi1GPIOBase = 0x3F200000
)

func DetectBase() (int64, error) {
	base := int64(pi1GPIOBase)

	const rangesPath = "/proc/device-tree/soc/ranges"
	ranges, err := os.Open(rangesPath)
	if err != nil {
		log.Printf("[gpio] DetectBase: open %s failed: %v (returning fallback base=0x%X)", rangesPath, err, base)
		return base, err
	}
	defer ranges.Close()

	b := make([]byte, 4)
	n, err := ranges.ReadAt(b, 4)
	if err != nil {
		log.Printf("[gpio] DetectBase: ReadAt(%s, off=4) failed: %v (n=%d, bytes=% X) fallback base=0x%X", rangesPath, err, n, b, base)
		return base, err
	}
	if n != 4 {
		err := fmt.Errorf("DT system on chip ranges is %d bytes instead of 4 bytes", n)
		log.Printf("[gpio] DetectBase: %v (bytes=% X) fallback base=0x%X", err, b, base)
		return base, err
	}

	buf := bytes.NewReader(b)
	var out uint32
	if err := binary.Read(buf, binary.BigEndian, &out); err != nil {
		// Note: your old code returned base,nil here which can mask the failure.
		log.Printf("[gpio] DetectBase: binary.Read failed: %v (bytes=% X) fallback base=0x%X", err, b, base)
		return base, err
	}

	// Your existing math:
	computed := int64(out + 0x200000)
	log.Printf("[gpio] DetectBase: ranges bytes=% X -> out=0x%X computed base=0x%X", b, out, computed)
	return computed, nil
}

func Mmap() ([]uint8, error) {
	const memLength = 4096

	base, err := DetectBase()
	if err != nil {
		log.Printf("[gpio] Mmap: DetectBase returned error: %v (base fallback might be wrong)", err)
	}

	// Try gpiomem first
	f, err := os.OpenFile("/dev/gpiomem", os.O_RDWR|os.O_SYNC, 0)
	var file *os.File
	switch {
	case os.IsNotExist(err):
		log.Printf("[gpio] Mmap: /dev/gpiomem does not exist; falling back to /dev/mem (requires root)")
		f1, err2 := os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
		if err2 != nil {
			log.Printf("[gpio] Mmap: open /dev/mem failed: %v", err2)
			return nil, err2
		}
		file = f1
	case err != nil:
		log.Printf("[gpio] Mmap: open /dev/gpiomem failed: %v", err)
		return nil, err
	default:
		log.Printf("[gpio] Mmap: using /dev/gpiomem")
		file = f
	}
	defer file.Close()

	log.Printf("[gpio] Mmap: attempting syscall.Mmap(fd=%d, offset=0x%X, len=%d)", file.Fd(), base, memLength)

	mem8, err := syscall.Mmap(
		int(file.Fd()),
		base,
		memLength,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		log.Printf("[gpio] Mmap: syscall.Mmap failed: %v (offset=0x%X)", err, base)
		return nil, err
	}

	log.Printf("[gpio] Mmap: OK mapped %d bytes at offset=0x%X", len(mem8), base)
	return mem8, nil
}
