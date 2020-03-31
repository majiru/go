package draw

import (
	"fmt"
	"image"
	"log"
	"os"
)

/*
* The api does not pass the mousectl for certain functions
* where we need access to the various files, so we stash
* away our most recent Moustctl here for use in these functions.
 */
var globalmc *Mousectl

// Mouse is the structure describing the current state of the mouse.
type Mouse struct {
	image.Point        // Location.
	Buttons     int    // Buttons; bit 0 is button 1, bit 1 is button 2, etc.
	Msec        uint32 // Time stamp in milliseconds.
}

// TODO: Mouse field is racy but okay.

// Mousectl holds the interface to receive mouse events.
// The Mousectl's Mouse is updated after send so it doesn't
// have the wrong value if the sending goroutine blocks during send.
// This means that programs should receive into Mousectl.Mouse
//  if they want full synchrony.
type Mousectl struct {
	Mouse                // Store Mouse events here.
	C       <-chan Mouse // Channel of Mouse events.
	Resize  <-chan bool  // Each received value signals a window resize (see the display.Attach method).
	Display *Display     // The associated display.

	mfile, cfile *os.File
}

// InitMouse connects to the mouse and returns a Mousectl to interact with it.
// We should return an error along with *Mousectl, instead we fatal
// to keep compatability.
func (d *Display) InitMouse() *Mousectl {
	var err error
	ch := make(chan Mouse, 0)
	rch := make(chan bool, 2)
	mc := &Mousectl{
		C:       ch,
		Resize:  rch,
		Display: d,
	}
	mc.mfile, err = os.OpenFile("/dev/mouse", os.O_RDWR, 0666)
	if err != nil {
		log.Fatal(err)
	}
	mc.cfile, err = os.OpenFile("/dev/cursor", os.O_RDWR, 0666)
	if err != nil {
		log.Fatal(err)
	}
	go mouseproc(mc, d, ch, rch)
	return mc
}

func mouseproc(mc *Mousectl, d *Display, ch chan Mouse, rch chan bool) {
	b := make([]byte, 1+5*12)
	for {
		_, err := mc.mfile.Read(b)
		if err != nil {
			log.Fatal(err)
		}
		switch rune(b[0]) {
		case 'r':
			rch <- true
			fallthrough
		case 'm':
			mm := Mouse{
				image.Point{atoi(b[1:]), atoi(b[1+1*12:])},
				atoi(b[1+2*12:]),
				uint32(atoll(b[1+3*12:])),
			}
			ch <- mm
			mc.Mouse = mm
		}
	}
}

// Read returns the next mouse event.
func (mc *Mousectl) Read() Mouse {
	mc.Display.Flush()
	m := <-mc.C
	mc.Mouse = m
	return m
}

// MoveTo moves the mouse cursor to the specified location.
func (d *Display) MoveTo(pt image.Point) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := globalmc.mfile.Write([]byte(fmt.Sprintf("m%d %d", pt.X, pt.Y)))
	globalmc.Mouse.Point = pt
	return err
}

// SetCursor sets the mouse cursor to the specified cursor image.
// SetCursor(nil) changes the cursor to the standard system cursor.
func (d *Display) SetCursor(c *Cursor) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	b := make([]byte, 2*4+2*2*16)
	if c == nil {
		_, err := globalmc.cfile.Write([]byte{})
		return err
	}
	bplong(b, uint32(c.X))
	bplong(b[1*4:], uint32(c.Y))
	copy(b[2*4:], c.Clr[:])
	_, err := globalmc.cfile.Write(b)
	return err
}

func atoll(b []byte) int32 {
	var i int32 = 0
	for i < int32(len(b)) && b[i] == ' ' {
		i++
	}
	var n int32 = 0
	for ; i < int32(len(b)) && '0' <= b[i] && b[i] <= '9'; i++ {
		n = n*10 + int32(b[i]) - '0'
	}
	return n
}
