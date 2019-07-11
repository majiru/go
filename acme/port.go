package acme // import "9fans.net/go/acme"

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var windowsMu sync.Mutex
var windows, last *Win
var autoExit bool

var fsysErr error
var fsysOnce sync.Once

// AutoExit sets whether to call os.Exit the next time the last managed acme window is deleted.
// If there are no acme windows at the time of the call, the exit does not happen until one
// is created and then deleted.
func AutoExit(exit bool) {
	windowsMu.Lock()
	defer windowsMu.Unlock()
	autoExit = exit
}

type LogEvent struct {
	ID   int
	Op   string
	Name string
}

type WinInfo struct {
	ID   int
	Name string
}

func (r *LogReader) Close() error {
	return r.f.Close()
}

// Read reads an event from the acme log file.
func (r *LogReader) Read() (LogEvent, error) {
	n, err := r.f.Read(r.buf[:])
	if err != nil {
		return LogEvent{}, err
	}
	f := strings.SplitN(string(r.buf[:n]), " ", 3)
	if len(f) != 3 {
		return LogEvent{}, fmt.Errorf("malformed log event")
	}
	id, _ := strconv.Atoi(f[0])
	op := f[1]
	name := f[2]
	name = strings.TrimSpace(name)
	return LogEvent{id, op, name}, nil
}

// Show looks and causes acme to show the window with the given name,
// returning that window.
// If this process has not created a window with the given name
// (or if any such window has since been deleted),
// Show returns nil.
func Show(name string) *Win {
	windowsMu.Lock()
	defer windowsMu.Unlock()

	for w := windows; w != nil; w = w.next {
		if w.name == name {
			if err := w.Ctl("show"); err != nil {
				w.dropLocked()
				return nil
			}
			return w
		}
	}
	return nil
}

// Addr writes format, ... to the window's addr file.
func (w *Win) Addr(format string, args ...interface{}) error {
	return w.Fprintf("addr", format, args...)
}

// CloseFiles closes all the open files associated with the window w.
// (These file descriptors are cached across calls to Ctl, etc.)
func (w *Win) CloseFiles() {
	w.ctl.Close()
	w.ctl = nil

	w.body.Close()
	w.body = nil

	w.addr.Close()
	w.addr = nil

	w.tag.Close()
	w.tag = nil

	w.event.Close()
	w.event = nil
	w.ebuf = nil

	w.data.Close()
	w.data = nil

	w.xdata.Close()
	w.xdata = nil

	w.errors.Close()
	w.errors = nil
}

// Ctl writes the command format, ... to the window's ctl file.
func (w *Win) Ctl(format string, args ...interface{}) error {
	return w.Fprintf("ctl", format+"\n", args...)
}

// Winctl deletes the window, writing `del' (or, if sure is true, `delete') to the ctl file.
func (w *Win) Del(sure bool) error {
	cmd := "del"
	if sure {
		cmd = "delete"
	}
	return w.Ctl(cmd)
}

// DeleteAll deletes all windows.
func DeleteAll() {
	for w := windows; w != nil; w = w.next {
		w.Ctl("delete")
	}
}

func (w *Win) OpenEvent() error {
	_, err := w.fid("event")
	return err
}

// ReadAll
func (w *Win) ReadAll(file string) ([]byte, error) {
	f, err := w.fid(file)
	f.Seek(0, 0)
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(f)
}

func (w *Win) ID() int {
	return w.id
}

func (w *Win) Name(format string, args ...interface{}) error {
	name := fmt.Sprintf(format, args...)
	if err := w.Ctl("name %s", name); err != nil {
		return err
	}
	w.name = name
	return nil
}

func (w *Win) Fprintf(file, format string, args ...interface{}) error {
	f, err := w.fid(file)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, format, args...)
	_, err = f.Write(buf.Bytes())
	return err
}

func (w *Win) Read(file string, b []byte) (n int, err error) {
	f, err := w.fid(file)
	if err != nil {
		return 0, err
	}
	return f.Read(b)
}

func (w *Win) ReadAddr() (q0, q1 int, err error) {
	f, err := w.fid("addr")
	if err != nil {
		return 0, 0, err
	}
	buf := make([]byte, 40)
	n, err := f.ReadAt(buf, 0)
	if err != nil {
		return 0, 0, err
	}
	a := strings.Fields(string(buf[0:n]))
	if len(a) < 2 {
		return 0, 0, errors.New("short read from acme addr")
	}
	q0, err0 := strconv.Atoi(a[0])
	q1, err1 := strconv.Atoi(a[1])
	if err0 != nil || err1 != nil {
		return 0, 0, errors.New("invalid read from acme addr")
	}
	return q0, q1, nil
}

func (w *Win) Seek(file string, offset int64, whence int) (int64, error) {
	f, err := w.fid(file)
	if err != nil {
		return 0, err
	}
	return f.Seek(offset, whence)
}

func (w *Win) Write(file string, b []byte) (n int, err error) {
	f, err := w.fid(file)
	if err != nil {
		return 0, err
	}
	return f.Write(b)
}

const eventSize = 256

// An Event represents an event originating in a particular window.
// The fields correspond to the fields in acme's event messages.
// See http://swtch.com/plan9port/man/man4/acme.html for details.
type Event struct {
	// The two event characters, indicating origin and type of action
	C1, C2 rune

	// The character addresses of the action.
	// If the original event had an empty selection (OrigQ0=OrigQ1)
	// and was accompanied by an expansion (the 2 bit is set in Flag),
	// then Q0 and Q1 will indicate the expansion rather than the
	// original event.
	Q0, Q1 int

	// The Q0 and Q1 of the original event, even if it was expanded.
	// If there was no expansion, OrigQ0=Q0 and OrigQ1=Q1.
	OrigQ0, OrigQ1 int

	// The flag bits.
	Flag int

	// The number of bytes in the optional text.
	Nb int

	// The number of characters (UTF-8 sequences) in the optional text.
	Nr int

	// The optional text itself, encoded in UTF-8.
	Text []byte

	// The chorded argument, if present (the 8 bit is set in the flag).
	Arg []byte

	// The chorded location, if present (the 8 bit is set in the flag).
	Loc []byte
}

// ReadEvent reads the next event from the window's event file.
func (w *Win) ReadEvent() (e *Event, err error) {
	defer func() {
		if v := recover(); v != nil {
			e = nil
			err = errors.New("malformed acme event: " + v.(string))
		}
	}()

	if _, err = w.fid("event"); err != nil {
		return nil, err
	}

	e = new(Event)
	w.gete(e)
	e.OrigQ0 = e.Q0
	e.OrigQ1 = e.Q1

	// expansion
	if e.Flag&2 != 0 {
		e2 := new(Event)
		w.gete(e2)
		if e.Q0 == e.Q1 {
			e2.OrigQ0 = e.Q0
			e2.OrigQ1 = e.Q1
			e2.Flag = e.Flag
			e = e2
		}
	}

	// chorded argument
	if e.Flag&8 != 0 {
		e3 := new(Event)
		e4 := new(Event)
		w.gete(e3)
		w.gete(e4)
		e.Arg = e3.Text
		e.Loc = e4.Text
	}

	return e, nil
}

func (w *Win) gete(e *Event) {
	if w.ebuf == nil {
		w.ebuf = bufio.NewReader(w.event)
	}
	e.C1 = w.getec()
	e.C2 = w.getec()
	e.Q0 = w.geten()
	e.Q1 = w.geten()
	e.Flag = w.geten()
	e.Nr = w.geten()
	if e.Nr > eventSize {
		panic("event string too long")
	}
	r := make([]rune, e.Nr)
	for i := 0; i < e.Nr; i++ {
		r[i] = w.getec()
	}
	e.Text = []byte(string(r))
	if w.getec() != '\n' {
		panic("phase error")
	}
}

func (w *Win) getec() rune {
	c, _, err := w.ebuf.ReadRune()
	if err != nil {
		panic(err.Error())
	}
	return c
}

func (w *Win) geten() int {
	var (
		c rune
		n int
	)
	for {
		c = w.getec()
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c) - '0'
	}
	if c != ' ' {
		panic("event number syntax")
	}
	return n
}

// WriteEvent writes an event back to the window's event file,
// indicating to acme that the event should be handled internally.
func (w *Win) WriteEvent(e *Event) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%c%c%d %d \n", e.C1, e.C2, e.Q0, e.Q1)
	_, err := w.Write("event", buf.Bytes())
	return err
}

// EventChan returns a channel on which events can be read.
// The first call to EventChan allocates a channel and starts a
// new goroutine that loops calling ReadEvent and sending
// the result into the channel.  Subsequent calls return the
// same channel.  Clients should not call ReadEvent after calling
// EventChan.
func (w *Win) EventChan() <-chan *Event {
	if w.c == nil {
		w.c = make(chan *Event, 0)
		go w.eventReader()
	}
	return w.c
}

func (w *Win) eventReader() {
	for {
		e, err := w.ReadEvent()
		if err != nil {
			break
		}
		w.c <- e
	}
	w.c <- new(Event) // make sure event reader is done processing last event; drop might exit
	w.drop()
	close(w.c)
}

func (w *Win) drop() {
	windowsMu.Lock()
	defer windowsMu.Unlock()
	w.dropLocked()
}

func (w *Win) dropLocked() {
	if w.prev == nil && w.next == nil && windows != w {
		return
	}
	if w.prev != nil {
		w.prev.next = w.next
	} else {
		windows = w.next
	}
	if w.next != nil {
		w.next.prev = w.prev
	} else {
		last = w.prev
	}
	w.prev = nil
	w.next = nil
	if autoExit && windows == nil {
		os.Exit(0)
	}
}

// Blink starts the window tag blinking and returns a function that stops it.
// When stop returns, the blinking is over and the window state is clean.
func (w *Win) Blink() (stop func()) {
	c := make(chan struct{})
	go func() {
		t := time.NewTicker(1000 * time.Millisecond)
		defer t.Stop()
		dirty := false
		for {
			select {
			case <-t.C:
				dirty = !dirty
				if dirty {
					w.Ctl("dirty")
				} else {
					w.Ctl("clean")
				}
			case <-c:
				w.Ctl("clean")
				c <- struct{}{}
				return
			}
		}
	}()
	return func() {
		c <- struct{}{}
		<-c
	}
}

// Sort sorts the lines in the current address range
// according to the comparison function.
func (w *Win) Sort(less func(x, y string) bool) error {
	q0, q1, err := w.ReadAddr()
	if err != nil {
		return err
	}
	data, err := w.ReadAll("xdata")
	if err != nil {
		return err
	}
	suffix := ""
	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] == "" {
		suffix = "\n"
		lines = lines[:len(lines)-1]
	}
	sort.SliceStable(lines, func(i, j int) bool { return less(lines[i], lines[j]) })
	w.Addr("#%d,#%d", q0, q1)
	w.Write("data", []byte(strings.Join(lines, "\n")+suffix))
	return nil
}

// Clear clears the window body.
func (w *Win) Clear() {
	w.Addr(",")
	w.Write("data", nil)
}

type EventHandler interface {
	Execute(cmd string) bool
	Look(arg string) bool
}

func (w *Win) loadText(e *Event, h EventHandler) {
	if len(e.Text) == 0 && e.Q0 < e.Q1 {
		w.Addr("#%d,#%d", e.Q0, e.Q1)
		data, err := w.ReadAll("xdata")
		if err != nil {
			w.Err(err.Error())
		}
		e.Text = data
	}
}

func (w *Win) EventLoop(h EventHandler) {
	for e := range w.EventChan() {
		switch e.C2 {
		case 'x', 'X': // execute
			cmd := strings.TrimSpace(string(e.Text))
			if !w.execute(h, cmd) {
				w.WriteEvent(e)
			}
		case 'l', 'L': // look
			// TODO(rsc): Expand selection, especially for URLs.
			w.loadText(e, h)
			if !h.Look(string(e.Text)) {
				w.WriteEvent(e)
			}
		}
	}
}

func (w *Win) execute(h EventHandler, cmd string) bool {
	verb, arg := cmd, ""
	if i := strings.IndexAny(verb, " \t"); i >= 0 {
		verb, arg = verb[:i], strings.TrimSpace(verb[i+1:])
	}

	// Look for specific method.
	m := reflect.ValueOf(h).MethodByName("Exec" + verb)
	if !m.IsValid() {
		// Fall back to general Execute.
		return h.Execute(cmd)
	}

	// Found method.
	// Committed to handling the event.
	// All returns below should be return true.

	// Check method signature.
	t := m.Type()
	switch t.NumOut() {
	default:
		w.Errf("bad method %s: too many results", cmd)
		return true
	case 0:
		// ok
	case 1:
		if t.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			w.Errf("bad method %s: return type %v, not error", cmd, t.Out(0))
			return true
		}
	}
	varg := reflect.ValueOf(arg)
	switch t.NumIn() {
	default:
		w.Errf("bad method %s: too many arguments", cmd)
		return true
	case 0:
		if arg != "" {
			w.Errf("%s takes no arguments", cmd)
			return true
		}
	case 1:
		if t.In(0) != varg.Type() {
			w.Errf("bad method %s: argument type %v, not string", cmd, t.In(0))
			return true
		}
	}

	args := []reflect.Value{}
	if t.NumIn() > 0 {
		args = append(args, varg)
	}
	out := m.Call(args)
	var err error
	if len(out) == 1 {
		err, _ = out[0].Interface().(error)
	}
	if err != nil {
		w.Errf("%v", err)
	}

	return true
}

func (w *Win) Selection() string {
	w.Ctl("addr=dot")
	data, err := w.ReadAll("xdata")
	if err != nil {
		w.Err(err.Error())
	}
	return string(data)
}

func (w *Win) SetErrorPrefix(p string) {
	w.errorPrefix = p
}

// Err finds or creates a window appropriate for showing errors related to w
// and then prints msg to that window.
// It adds a final newline to msg if needed.
func (w *Win) Err(msg string) {
	Err(w.errorPrefix, msg)
}

func (w *Win) Errf(format string, args ...interface{}) {
	w.Err(fmt.Sprintf(format, args...))
}

// Err finds or creates a window appropriate for showing errors related to a window titled src
// and then prints msg to that window. It adds a final newline to msg if needed.
func Err(src, msg string) {
	if !strings.HasSuffix(msg, "\n") {
		msg = msg + "\n"
	}
	prefix, _ := path.Split(src)
	if prefix == "/" || prefix == "." {
		prefix = ""
	}
	name := prefix + "+Errors"
	w1 := Show(name)
	if w1 == nil {
		var err error
		w1, err = New()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			w1, err = New()
			if err != nil {
				log.Fatalf("cannot create +Errors window")
			}
		}
		w1.Name("%s", name)
	}
	w1.Addr("$")
	w1.Ctl("dot=addr")
	w1.Fprintf("body", "%s", msg)
	w1.Addr(".,")
	w1.Ctl("dot=addr")
	w1.Ctl("show")
}

// Errf is like Err but accepts a printf-style formatting.
func Errf(src, format string, args ...interface{}) {
	Err(src, fmt.Sprintf(format, args...))
}
