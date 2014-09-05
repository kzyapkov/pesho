package door

import (
	"sync"
	"testing"

	"github.com/kzyapkov/gpio/test"
)

func getTestWires() *wires {
	w := &wires{
		locked:   &test.PinMock{},
		unlocked: &test.PinMock{},
		door:     &test.PinMock{},

		enable: &test.PinMock{},
		lock:   &test.PinMock{},
		unlock: &test.PinMock{},

		Mutex: &sync.Mutex{},
	}
	var a doorAutomata
	a = w // make sure we implement the interface
	_ = a
	return w
}

func TestCreatePins(t *testing.T) {
	var m doorAutomata
	m = getTestWires()
	m.stateDelegate(func(s State) {})
}

func TestInitialStateLocked(t *testing.T) {
	m := getTestWires()
	m.locked.(*test.PinMock).TheValue = true
	m.unlocked.(*test.PinMock).TheValue = false
	m.door.(*test.PinMock).TheValue = true
	var state State
	m.stateDelegate(func(s State) {
		state = s
	})

	m.reset()

	if state.latch != Locked {
		t.Error("latch should be locked")
	}
	if state.door != Closed {
		t.Error("door should be closed")
	}
}

func TestInitialStateInvalid(t *testing.T) {
	m := getTestWires()
	m.locked.(*test.PinMock).TheValue = false
	m.unlocked.(*test.PinMock).TheValue = false
	m.door.(*test.PinMock).TheValue = false
	var state State
	m.stateDelegate(func(s State) {
		state = s
	})

	m.reset()

	if state.latch != Unknown {
		t.Error("latch should be unknown")
	}
	if state.door != Open {
		t.Error("door should be open")
	}
}

func TestHandleLocked(t *testing.T) {

}
