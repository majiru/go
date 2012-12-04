package draw

import (
	"fmt"
	"io"
)

func (d *Display) readSubfont(name string, fd io.Reader, ai *Image, dolock bool) (*Subfont, error) {
	hdr := make([]byte, 3*12+4)
	i := ai
	if i == nil {
		var err error
		i, err = d.readImage(fd, dolock)
		if err != nil {
			return nil, err
		}
	}
	var (
		n   int
		p   []byte
		fc  []Fontchar
		f   *Subfont
		err error
	)
	if _, err = io.ReadFull(fd, hdr[:3*12]); err != nil {
		err = fmt.Errorf("rdsubfontfile: header read error: %r")
		goto Err
	}
	n = atoi(hdr)
	p = make([]byte, 6*(n+1))
	if _, err = io.ReadFull(fd, p); err != nil {
		err = fmt.Errorf("rdsubfontfile: fontchar read error: %r")
		goto Err
	}
	fc = make([]Fontchar, n+1)
	unpackinfo(fc, p, n)
	if dolock {
		// XXX
	}
	f = AllocSubfont(name, atoi(hdr[12:]), atoi(hdr[24:]), fc, i)
	if dolock {
		// XXX
	}
	return f, nil

Err:
	if ai == nil {
		i.Free()
	}
	return nil, err
}

func (d *Display) ReadSubfont(name string, fd io.Reader) (*Subfont, error) {
	return d.readSubfont(name, fd, nil, true)
}

func unpackinfo(fc []Fontchar, p []byte, n int) {
	for j := 0; j <= n; j++ {
		fc[j].X = int(p[0]) | int(p[1])<<8
		fc[j].Top = uint8(p[2])
		fc[j].Bottom = uint8(p[3])
		fc[j].Left = int8(p[4])
		fc[j].Width = uint8(p[5])
		p = p[6:]
	}
}