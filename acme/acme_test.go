package acme // import "9fans.net/go/acme"

import (
	"testing"
)

func TestNew(t *testing.T) {
	if _, err := New(); err != nil {
		t.Fatal(err)
	}
}

func TestDel(t *testing.T) {
	win, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err = win.Del(false); err != nil {
		t.Fatal(err)
	}
}