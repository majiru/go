package draw

import (
	"log"
	"os"
)

const (
	KeyFn = '\uF000'

	KeyHome      = KeyFn | 0x0D
	KeyUp        = KeyFn | 0x0E
	KeyPageUp    = KeyFn | 0xF
	KeyPrint     = KeyFn | 0x10
	KeyLeft      = KeyFn | 0x11
	KeyRight     = KeyFn | 0x12
	KeyDown      = 0x80
	KeyView      = 0x80
	KeyPageDown  = KeyFn | 0x13
	KeyInsert    = KeyFn | 0x14
	KeyEnd       = KeyFn | 0x18
	KeyAlt       = KeyFn | 0x15
	KeyShift     = KeyFn | 0x16
	KeyCtl       = KeyFn | 0x17
	KeyBackspace = 0x08
	KeyDelete    = 0x7F
	KeyEscape    = 0x1b
	KeyEOF       = 0x04
	KeyCmd       = 0xF100
)

// Keyboardctl is the source of keyboard events.
type Keyboardctl struct {
	C <-chan rune // Channel on which keyboard characters are delivered.

	ctl, cons *os.File
}

// InitKeyboard connects to the keyboard and returns a Keyboardctl to listen to it.
// Normally we would return an error, but to keep compatability with original code
// We simply fatal on an error.
func (d *Display) InitKeyboard() *Keyboardctl {
	var err error
	const rawon = "rawon"

	ch := make(chan rune, 20)
	k := &Keyboardctl{
		C: ch,
	}
	k.cons, err = os.OpenFile("/dev/cons", os.O_RDWR, 0666)
	if err != nil {
		log.Fatal(err)
	}
	k.ctl, err = os.OpenFile("/dev/consctl", os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = k.ctl.Write([]byte(rawon)); err != nil {
		log.Fatal(err)
	}
	go kbdproc(ch, k.cons)
	return k
}

func kbdproc(ch chan rune, cons *os.File) {
	b := make([]byte, 20)
	for {
		_, err := cons.Read(b)
		if err != nil {
			log.Fatal(err)
		}
		for _, r := range string(b) {
			ch <- r
		}
	}
}
