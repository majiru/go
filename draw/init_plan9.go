package draw

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/majiru/lib9"
)

type Display struct {
	Image       *Image
	Screen      *Screen
	ScreenImage *Image
	Windows     *Image
	DPI         int

	firstfont *Font
	lastfont  *Font

	White       *Image
	Black       *Image
	Opaque      *Image
	Transparent *Image

	DefaultFont    *Font
	DefaultSubfont *Subfont

	errch   chan<- error
	mu      sync.Mutex
	imageid uint32
	qmask   *Image

	ctl, data, ref *os.File
	dirno          int
	bufsize        int
	buf            []byte
	dataqid        uint64
	local          bool
	isnew          bool
	oldlabel       string
}

type Image struct {
	Display *Display
	Pix     Pix
	Depth   int
	Repl    bool
	R       image.Rectangle
	Clipr   image.Rectangle
	Screen  *Screen

	next *Image
	id   uint32
}

type Screen struct {
	Display *Display
	id      uint32
	Fill    *Image
}

const InfoSize = 12 * 12

// Refresh algorithms to execute when a window is resized or uncovered.
// Refmesg is almost always the correct one to use.
const (
	Refbackup = 0
	Refnone   = 1
	Refmesg   = 2
)

const deffontname = "*default*"

//Init sets up the screen and connects the specific window image specified
//by /dev/winname
func Init(errch chan<- error, fontname, label, winsize string) (*Display, error) {
	d, err := initdisplay(errch)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	/*
	 * Set up default font
	 */
	df, err := getdefont(d)
	if err != nil {
		return nil, err
	}
	d.DefaultSubfont = df

	if fontname == "" {
		fontname = os.Getenv("font")
	}

	/*
	 * Build fonts with caches==depth of screen, for speed.
	 * If conversion were faster, we'd use 0 and save memory.
	 */
	var font *Font
	if fontname == "" {
		buf := []byte(fmt.Sprintf("%d %d\n0 %d\t%s\n", df.Height, df.Ascent,
			df.N-1, deffontname))
		//fmt.Printf("%q\n", buf)
		//BUG: Need something better for this	installsubfont("*default*", df);
		font, err = d.buildFont(buf, deffontname)
	} else {
		font, err = d.openFont(fontname) // BUG: grey fonts
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "imageinit: can't open default font %s: %v\n", fontname, err)
		return nil, err
	}
	d.DefaultFont = font

	if _, err = os.Stat("/dev/label"); err == nil {
		f, err := os.Open("/dev/label")
		defer f.Close()
		if err != nil {
			return nil, err
		}
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, err
		}
		d.oldlabel = string(b)
	}

	if label != "" {
		f, err := os.Create("/dev/label")
		defer f.Close()
		if err != nil {
			return nil, err
		}
		f.Write([]byte(label))
	}

	return d, d.Attach(0)
}

func (d *Display) Close() error {
	defer d.ctl.Close()
	defer d.ref.Close()
	defer d.data.Close()

	if d.oldlabel != "" {
		f, err := os.OpenFile("/dev/label", os.O_RDWR, 0666)
		defer f.Close()
		if err != nil {
			return err
		}
		f.Write([]byte(d.oldlabel))
	}

	/*
	* This is a bit of a hack to force a redraw of the
	* window from rio. This can result in a small flicker
	* of the window, but I can't seem to find reference
	* of this action within the closedisplay functions
	* of /sys/src/libdraw/init.c for a proper way
	* of handeling this.
	 */
	f, err := os.OpenFile("/dev/wctl", os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	f.Write([]byte("hide"))
	f.Write([]byte("unhide"))
	f.Close()
	return nil
}

const noborder = "noborder"

//See /sys/src/libdraw/init.c:/^gengetwindow/
func (d *Display) Attach(ref int) error {
	var (
		b   []byte
		i   *Image = nil
		err error
	)

	f, oerr := os.Open("/dev/winname")
	if oerr == nil {
		b, err = ioutil.ReadAll(f)
		if err == nil {
			/*
			* There is a race where the name changes between
			* the time we read it and the time we try to pull it
			* from the draw device, so this should be wrapped.
			 */
			if i, err = d.namedimage(string(b)); err != nil {
				return err
			}
		}
	}
	if i == nil {
		//We are not running under a rio-like
		//Set image to whole display
		b = []byte(noborder)
		i = d.Image
	}

	d.Screen, err = i.allocScreen(d.White, false)
	if err != nil {
		return err
	}
	d.ScreenImage = i // temporary, for d.ScreenImage.Pix
	d.ScreenImage, err = allocwindow(nil, d.Screen, d.Image.R, ref, White)
	if err != nil {
		return err
	}
	if err := d.flush(true); err != nil {
		log.Fatal(err)
	}

	screen := i
	screen.draw(i.R, d.White, nil, image.ZP)
	if err := d.flush(true); err != nil {
		log.Fatal(err)
	}

	return nil
}

//See /sys/src/libdraw/init.c:/^initdisplay/
func initdisplay(errch chan<- error) (*Display, error) {
	var (
		err error
		b   []byte
		n   int
	)
	d := &Display{errch: errch}
	b = make([]byte, InfoSize+1)

	d.ctl, err = os.Open("/dev/draw/new")
	if err != nil {
		return nil, err
	}

	n, err = d.ctl.Read(b)
	if err != nil {
		return nil, err
	}
	if n == InfoSize+1 {
		n = InfoSize
	}
	if n < InfoSize {
		d.isnew = true
	}

	d.dirno = atoi(bytes.TrimSpace(b[:12]))

	d.data, err = os.OpenFile(fmt.Sprintf("/dev/draw/%d/data", d.dirno), os.O_RDWR, 0755)
	if err != nil {
		return nil, err
	}

	d.ref, err = os.Open(fmt.Sprintf("/dev/draw/%d/refresh", d.dirno))
	if err != nil {
		return nil, err
	}

	bs, err := lib9.Iounit(d.data.Fd())
	if err != nil {
		d.bufsize = 8000
	} else {
		d.bufsize = int(bs)
	}
	d.buf = make([]byte, d.bufsize+5) /* +5 for flush message */

	var i *Image = nil
	if n >= InfoSize {
		pix, _ := ParsePix(strings.TrimSpace(string(b[2*12 : 3*12])))
		i = &Image{
			Display: d,
			id:      0,
			Pix:     pix,
			Depth:   pix.Depth(),
			Repl:    atoi(b[3*12:]) > 0,
			R:       ator(b[4*12:]),
			Clipr:   ator(b[8*12:]),
		}
	}
	d.Image = i
	d.White, err = d.allocImage(image.Rect(0, 0, 1, 1), GREY1, true, White)
	if err != nil {
		return nil, err
	}
	d.Black, err = d.allocImage(image.Rect(0, 0, 1, 1), GREY1, true, Black)
	if err != nil {
		return nil, err
	}
	d.Opaque = d.White
	d.Transparent = d.Black

	ctlDir, err := lib9.Dirfstat(d.ctl)
	if err != nil {
		return nil, err
	}
	if ctlDir.Type == 'i' {
		d.local = true
		d.dataqid = ctlDir.Qid.Path
	}
	if ctlDir.Qid.Vers == 1 {
		d.isnew = true
	}

	return d, nil
}

//See /sys/src/libdraw/init.c:/^doflush/
func (d *Display) flushBuffer() error {
	if len(d.buf) == 0 {
		return nil
	}
	_, err := d.data.Write(d.buf)
	d.buf = d.buf[:0]
	return err
}

//See /sys/src/libdraw/init.c:/^flushimage/
func (d *Display) flush(visible bool) error {
	if visible {
		d.bufsize++
		a := d.bufimage(1)
		d.bufsize--
		a[0] = 'v'
		//NOTE might need to do something with d.isnew here
	}

	return d.flushBuffer()
}

func (d *Display) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.flush(true)
}

//See /sys/src/libdraw/init.c:/^bufimage/
func (d *Display) bufimage(n int) []byte {
	if d == nil || n < 0 || n > d.bufsize {
		panic("bad count in bufimage")
	}
	if len(d.buf)+n > d.bufsize {
		if err := d.flushBuffer(); err != nil {
			panic("bufimage flush: " + err.Error())
		}
	}
	i := len(d.buf)
	d.buf = d.buf[:i+n]
	return d.buf[i:]
}

func (d *Display) HiDPI() bool { return false }

func atoi(b []byte) int {
	i := 0
	for i < len(b) && b[i] == ' ' {
		i++
	}
	n := 0
	for ; i < len(b) && '0' <= b[i] && b[i] <= '9'; i++ {
		n = n*10 + int(b[i]) - '0'
	}
	return n
}

func atop(b []byte) image.Point {
	return image.Pt(atoi(b), atoi(b[12:]))
}

func ator(b []byte) image.Rectangle {
	return image.Rectangle{atop(b), atop(b[2*12:])}
}

func bplong(b []byte, n uint32) {
	binary.LittleEndian.PutUint32(b, n)
}

func bpshort(b []byte, n uint16) {
	binary.LittleEndian.PutUint16(b, n)
}
