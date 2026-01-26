//go:build !windows
// +build !windows

package i2c

import (
	"fmt"
	"io"
	"os"
	"log"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	delay    = 20
	slaveCmd = 0x0703 // Cmd to set slave address
	rdrwCmd  = 0x0707 // Cmd to read/write data together
	rd       = 0x0001
)

type Fd interface {
	io.ReadWriteCloser
	Fd() uintptr
}

type message struct {
	addr  uint16
	flags uint16
	len   uint16
	buf   uintptr
}

type ioctlData struct {
	msgs uintptr
	nmsg uint32
}

type Bus interface {
	SetAddress(addr byte) error
	ReadBytes(addr byte, num int) ([]byte, error)
	WriteBytes(addr byte, value []byte) error
	ReadFromReg(addr, reg byte, value []byte) error
	WriteToReg(addr, reg byte, value []byte) error
	Close() error
}

type bus struct {
	f         Fd
	syscallFn func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)
	mu        *sync.Mutex
}

func New() (*bus, error) {
log.Printf("I2C DEBUG: using FIXED rpi/i2c implementation (i2c-1)")
	f, err := os.OpenFile("/dev/i2c-1", os.O_RDWR, os.ModeExclusive)
	if err != nil {
		return nil, err
	}
	return &bus{f: f, mu: new(sync.Mutex), syscallFn: syscall.Syscall}, nil
}

func (b *bus) send(cmd, addr uintptr) error {
	if _, _, errno := b.syscallFn(syscall.SYS_IOCTL, b.f.Fd(), cmd, addr); errno != 0 {
		return syscall.Errno(errno)
	}
	return nil
}

func (b *bus) Close() error {
	return b.f.Close()
}

func (b *bus) SetAddress(addr byte) error {
	return b.send(slaveCmd, uintptr(addr))
}

func (b *bus) ReadBytes(addr byte, num int) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.SetAddress(addr); err != nil {
		return []byte{0}, err
	}
	bytes := make([]byte, num)
	n, err := b.f.Read(bytes)
	if err != nil {
		return nil, err
	}
	if n != num {
		return []byte{0}, fmt.Errorf("i2c: unexpected number (%v) of bytes read", n)
	}
	return bytes, nil
}

func (b *bus) WriteBytes(addr byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.SetAddress(addr); err != nil {
		return err
	}
	_, err := b.f.Write(value)
	return err
}

// ReadFromReg performs a proper I2C_RDWR combined transaction:
//  1) write 1 byte register (pointer)
//  2) read N bytes
//
// This avoids unsafe reflect.SliceHeader usage and avoids passing &reg (stack byte)
// directly to ioctl, which can lead to silent 0x0000 reads on some systems.
func (b *bus) ReadFromReg(addr, reg byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// stable 1-byte buffer for the register/pointer
	regbuf := []byte{reg}

	var msgs [2]message
	msgs[0].addr = uint16(addr)
	msgs[0].flags = 0
	msgs[0].len = 1
	msgs[0].buf = uintptr(unsafe.Pointer(&regbuf[0]))

	msgs[1].addr = uint16(addr)
	msgs[1].flags = rd
	msgs[1].len = uint16(len(value))
	if len(value) > 0 {
		msgs[1].buf = uintptr(unsafe.Pointer(&value[0]))
	} else {
		msgs[1].buf = 0
	}

	d := ioctlData{
		msgs: uintptr(unsafe.Pointer(&msgs[0])),
		nmsg: 2,
	}

	// NOTE: SetAddress is not required for I2C_RDWR (each message has addr).
	// Calling it is harmless on most systems, but can cause weirdness on some.
	if err := b.send(rdrwCmd, uintptr(unsafe.Pointer(&d))); err != nil {
		return err
	}

	// Ensure the GC keeps these alive until after ioctl completes.
	runtime.KeepAlive(regbuf)
	runtime.KeepAlive(value)

	return nil
}

// WriteToReg writes register + bytes using a single I2C_RDWR message.
// Avoids append()+reflect.SliceHeader unsafe pointer tricks.
func (b *bus) WriteToReg(addr, reg byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	outbuf := make([]byte, 1+len(value))
	outbuf[0] = reg
	copy(outbuf[1:], value)

	var msg message
	msg.addr = uint16(addr)
	msg.flags = 0
	msg.len = uint16(len(outbuf))
	msg.buf = uintptr(unsafe.Pointer(&outbuf[0]))

	d := ioctlData{
		msgs: uintptr(unsafe.Pointer(&msg)),
		nmsg: 1,
	}

	if err := b.send(rdrwCmd, uintptr(unsafe.Pointer(&d))); err != nil {
		return err
	}

	runtime.KeepAlive(outbuf)
	return nil
}
