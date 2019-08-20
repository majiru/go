package draw

import (
	"fmt"
	"strings"
)

func (d *Display) namedimage(name string) (*Image, error) {
	const readsize = InfoSize + 1
	var i *Image

	if len(name) >= 256 {
		return nil, fmt.Errorf("namedimage: Name too long")
	}
	/* flush pending data so we don't get error allocating the image */
	d.flush(false)
	a := d.bufimage(1 + 4 + 1 + len(name))
	d.imageid++
	id := d.imageid
	a[0] = 'n'
	bplong(a[1:], id)
	a[5] = byte(len(name))
	copy(a[6:], name)
	if err := d.flush(false); err != nil {
		return nil, err
	}

	b := make([]byte, readsize)
	if n, err := d.ctl.Read(b); err != nil || n < InfoSize {
		return nil, err
	}
	pix, _ := ParsePix(strings.TrimSpace(string(b[2*12 : 3*12])))
	i = &Image{
		Display: d,
		id:      id,
		Pix:     pix,
		Depth:   pix.Depth(),
		Repl:    atoi(b[3*12:]) > 0,
		R:       ator(b[4*12:]),
		Clipr:   ator(b[8*12:]),
	}
	return i, nil
}

func (d *Display) nameimage(i *Image, name string, in bool) error {
	a := i.Display.bufimage(1 + 4 + 1 + 1 + len(name))
	a[0] = 'N'
	bplong(a[1:], i.id)
	if in {
		a[5] = 1
	}
	a[6] = byte(len(name))
	copy(a[7:], name)
	return d.flush(false)
}
