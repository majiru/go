package acme // import "9fans.net/go/acme"

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

type Win struct {
	id         int
	ctl        *os.File
	tag        *os.File
	body       *os.File
	addr       *os.File
	event      *os.File
	data       *os.File
	xdata      *os.File
	errors     *os.File
	ebuf       *bufio.Reader
	c          chan *Event
	next, prev *Win
	buf        []byte
	e2, e3, e4 Event
	name       string

	errorPrefix string
}

func mountAcme() {
	_, fsysErr = os.Stat("/mnt/acme")
}

// New creates a new window.
func New() (*Win, error) {
	fsysOnce.Do(mountAcme)
	if fsysErr != nil {
		return nil, fsysErr
	}
	fid, err := os.OpenFile("/mnt/acme/new/ctl", os.O_RDWR, 0755)
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
	f   *os.File
	buf [8192]byte
}

// Log returns a reader reading the acme/log file.
func Log() (*LogReader, error) {
	return nil, errors.New("not supported")
}

// Windows returns a list of the existing acme windows.
func Windows() ([]WinInfo, error) {
	fsysOnce.Do(mountAcme)
	if fsysErr != nil {
		return nil, fsysErr
	}
	index, err := os.Open("/mnt/acme/index")
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
func Open(id int, ctl *os.File) (*Win, error) {
	fsysOnce.Do(mountAcme)
	if fsysErr != nil {
		return nil, fsysErr
	}
	if ctl == nil {
		var err error
		ctl, err = os.OpenFile(fmt.Sprintf("/mnt/acme/%d/ctl", id), os.O_RDWR, 0755)
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

func (w *Win) fid(name string) (*os.File, error) {
	var f **os.File
	var mode int = os.O_RDWR
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
		mode = os.O_WRONLY
	default:
		return nil, errors.New("unknown acme file: " + name)
	}
	if *f == nil {
		var err error
		*f, err = os.OpenFile(fmt.Sprintf("/mnt/acme/%d/%s", w.id, name), mode, 0755)
		if err != nil {
			return nil, err
		}
	}
	return *f, nil
}
