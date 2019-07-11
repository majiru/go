// +build !plan9

// Package acme is a simple interface for interacting with acme windows.
//
// Many of the functions in this package take a format string and optional
// parameters.  In the documentation, the notation format, ... denotes the result
// of formatting the string and arguments using fmt.Sprintf.
package acme // import "9fans.net/go/acme"

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"sync"

	"9fans.net/go/draw"
	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
)

// A Win represents a single acme window and its control files.
type Win struct {
	id         int
	ctl        *client.Fid
	tag        *client.Fid
	body       *client.Fid
	addr       *client.Fid
	event      *client.Fid
	data       *client.Fid
	xdata      *client.Fid
	errors     *client.Fid
	ebuf       *bufio.Reader
	c          chan *Event
	next, prev *Win
	buf        []byte
	e2, e3, e4 Event
	name       string

	errorPrefix string
}

var fsys *client.Fsys

func mountAcme() {
	fsys, fsysErr = client.MountService("acme")
}

// New creates a new window.
func New() (*Win, error) {
	fsysOnce.Do(mountAcme)
	if fsysErr != nil {
		return nil, fsysErr
	}
	fid, err := fsys.Open("new/ctl", plan9.ORDWR)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 100)
	n, err := fid.Read(buf)
	if err != nil {
		fid.Close()
		return nil, err
	}
	a := strings.Fields(string(buf[0:n]))
	if len(a) == 0 {
		fid.Close()
		return nil, errors.New("short read from acme/new/ctl")
	}
	id, err := strconv.Atoi(a[0])
	if err != nil {
		fid.Close()
		return nil, errors.New("invalid window id in acme/new/ctl: " + a[0])
	}
	return Open(id, fid)
}

// A LogReader provides read access to the acme log file.
type LogReader struct {
	f   *client.Fid
	buf [8192]byte
}

// Log returns a reader reading the acme/log file.
func Log() (*LogReader, error) {
	fsysOnce.Do(mountAcme)
	if fsysErr != nil {
		return nil, fsysErr
	}
	f, err := fsys.Open("log", plan9.OREAD)
	if err != nil {
		return nil, err
	}
	return &LogReader{f: f}, nil
}

// Windows returns a list of the existing acme windows.
func Windows() ([]WinInfo, error) {
	fsysOnce.Do(mountAcme)
	if fsysErr != nil {
		return nil, fsysErr
	}
	index, err := fsys.Open("index", plan9.OREAD)
	if err != nil {
		return nil, err
	}
	defer index.Close()
	data, err := ioutil.ReadAll(index)
	if err != nil {
		return nil, err
	}
	var info []WinInfo
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		n, _ := strconv.Atoi(f[0])
		info = append(info, WinInfo{n, f[5]})
	}
	return info, nil
}

// Open connects to the existing window with the given id.
// If ctl is non-nil, Open uses it as the window's control file
// and takes ownership of it.
func Open(id int, ctl *client.Fid) (*Win, error) {
	fsysOnce.Do(mountAcme)
	if fsysErr != nil {
		return nil, fsysErr
	}
	if ctl == nil {
		var err error
		ctl, err = fsys.Open(fmt.Sprintf("%d/ctl", id), plan9.ORDWR)
		if err != nil {
			return nil, err
		}
	}

	w := new(Win)
	w.id = id
	w.ctl = ctl
	w.next = nil
	w.prev = last
	if last != nil {
		last.next = w
	} else {
		windows = w
	}
	last = w
	return w, nil
}

func (w *Win) fid(name string) (*client.Fid, error) {
	var f **client.Fid
	var mode uint8 = plan9.ORDWR
	switch name {
	case "addr":
		f = &w.addr
	case "body":
		f = &w.body
	case "ctl":
		f = &w.ctl
	case "data":
		f = &w.data
	case "event":
		f = &w.event
	case "tag":
		f = &w.tag
	case "xdata":
		f = &w.xdata
	case "errors":
		f = &w.errors
		mode = plan9.OWRITE
	default:
		return nil, errors.New("unknown acme file: " + name)
	}
	if *f == nil {
		var err error
		*f, err = fsys.Open(fmt.Sprintf("%d/%s", w.id, name), mode)
		if err != nil {
			return nil, err
		}
	}
	return *f, nil
}

// PrintTabbed prints tab-separated columnated text to body,
// replacing single tabs with runs of tabs as needed to align columns.
func (w *Win) PrintTabbed(text string) {
	tab, font, _ := w.Font()

	lines := strings.SplitAfter(text, "\n")
	var allRows [][]string
	for _, line := range lines {
		if line == "" {
			continue
		}
		line = strings.TrimSuffix(line, "\n")
		allRows = append(allRows, strings.Split(line, "\t"))
	}

	var buf bytes.Buffer
	for len(allRows) > 0 {
		if row := allRows[0]; len(row) <= 1 {
			if len(row) > 0 {
				buf.WriteString(row[0])
			}
			buf.WriteString("\n")
			allRows = allRows[1:]
			continue
		}

		i := 0
		for i < len(allRows) && len(allRows[i]) > 1 {
			i++
		}

		rows := allRows[:i]
		allRows = allRows[i:]

		var wid []int
		if font != nil {
			for _, row := range rows {
				for len(wid) < len(row) {
					wid = append(wid, 0)
				}
				for i, col := range row {
					n := font.StringWidth(col)
					if wid[i] < n {
						wid[i] = n
					}
				}
			}
		}

		for _, row := range rows {
			for i, col := range row {
				buf.WriteString(col)
				if i == len(row)-1 {
					break
				}
				if font == nil || tab == 0 {
					buf.WriteString("\t")
					continue
				}
				pos := font.StringWidth(col)
				for pos <= wid[i] {
					buf.WriteString("\t")
					pos += tab - pos%tab
				}
			}
			buf.WriteString("\n")
		}
	}

	w.Write("body", buf.Bytes())
}

var fontCache struct {
	sync.Mutex
	m map[string]*draw.Font
}

// Font returns the window's current tab width (in zeros) and font.
func (w *Win) Font() (tab int, font *draw.Font, err error) {
	ctl := make([]byte, 1000)
	w.Seek("ctl", 0, 0)
	n, err := w.Read("ctl", ctl)
	if err != nil {
		return 0, nil, err
	}
	f := strings.Fields(string(ctl[:n]))
	if len(f) < 8 {
		return 0, nil, fmt.Errorf("malformed ctl file")
	}
	tab, _ = strconv.Atoi(f[7])
	if tab == 0 {
		return 0, nil, fmt.Errorf("malformed ctl file")
	}
	name := f[6]

	fontCache.Lock()
	font = fontCache.m[name]
	fontCache.Unlock()

	if font != nil {
		return tab, font, nil
	}

	var disp *draw.Display = nil
	font, err = disp.OpenFont(name)
	if err != nil {
		return tab, nil, err
	}

	fontCache.Lock()
	if fontCache.m == nil {
		fontCache.m = make(map[string]*draw.Font)
	}
	if fontCache.m[name] != nil {
		font = fontCache.m[name]
	} else {
		fontCache.m[name] = font
	}
	fontCache.Unlock()

	return tab, font, nil
}
