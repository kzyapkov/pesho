package door

import (
	"sync"
	"testing"

	"github.com/kzyapkov/gpio/test"
)

func getTestWires() *wires {
	w := &wires{
		sensors: &sensors{
			locked:   &test.PinMock{},
			unlocked: &test.PinMock{},
			door:     &test.PinMock{},
		},
		controls: &controls{
			enable: &test.PinMock{},
			lock:   &test.PinMock{},
			unlock: &test.PinMock{},
		},
		mutex: &sync.Mutex{},
	}
	var a doorAutomata
	a = w // make sure we implement the interface
	_ = a
	return w
}

func initWires(w *wires, locked bool) {
	w.locked.(*test.PinMock).TheValue = false
	w.unlocked.(*test.PinMock).TheValue = false
	w.door.(*test.PinMock).TheValue = false

	w.reset()
}

func stateMonitor(w *wires) *State {
	var state State
	w.setDelegate(func(s State) {
		state = s
	})
	return &state
}

func TestGettingTestWires(t *testing.T) {
	var m doorAutomata
	m = getTestWires()
	m.setDelegate(func(s State) {})
}

func TestSensorsReadDoor(t *testing.T) {
	c := sensors{
		&test.PinMock{},
		&test.PinMock{},
		&test.PinMock{},
	}

	c.door.(*test.PinMock).TheValue = true
	isClosed := c.readDoor()
	if isClosed != true {
		t.Fail()
	}
	state := &State{}
	c.updateDoorState(state)
	if state.Door != Closed {
		t.Fail()
	}
}

func TestInitialLatchStates(t *testing.T) {
	m := getTestWires()
	m.sensors.locked.(*test.PinMock).TheValue = true
	m.sensors.unlocked.(*test.PinMock).TheValue = false
	state := stateMonitor(m)

	m.reset()
	if state.Latch != Locked {
		t.Errorf("want state.Latch=(%v), have (%v)", Locked, state.Latch)
	}

	m.sensors.locked.(*test.PinMock).TheValue = false
	m.sensors.unlocked.(*test.PinMock).TheValue = true

	m.reset()
	if state.Latch != Unlocked {
		t.Errorf("latch should be locked")
	}

}

func TestInitialStateInvalid(t *testing.T) {
	m := getTestWires()
	m.locked.(*test.PinMock).TheValue = false
	m.unlocked.(*test.PinMock).TheValue = false
	m.door.(*test.PinMock).TheValue = false
	state := stateMonitor(m)

	m.reset()

	if state.Latch != Unknown {
		t.Error("latch should be unknown")
	}
	if state.Door != Open {
		t.Error("door should be open")
	}
}

func TestInitialDoorStates(t *testing.T) {
	m := getTestWires()
	m.door.(*test.PinMock).TheValue = false
	state := stateMonitor(m)
	m.reset()
	if state.Door != Open {
		t.Fail()
	}
	m.door.(*test.PinMock).TheValue = true
	m.reset()
	if state.Door != Closed {
		t.Fail()
	}
}

func TestHandleLocked(t *testing.T) {
	m := getTestWires()

	// straight-forward case, locked triggered while locking
	m.sensors.locked.(*test.PinMock).TheValue = false
	m.sensors.unlocked.(*test.PinMock).TheValue = true
	m.controls.enable.(*test.PinMock).TheValue = true
	state := stateMonitor(m)
	m.reset()
	m.state.Latch = Locking
	m.handleSenseLocked()
	if state.Latch != Locked {
		t.Errorf("want state.Latch=(%v), have (%v)", Locked, state.Latch)
	}
	if m.controls.enable.Get() {
		t.Errorf("the motor should have been stopped!")
	}

	// TODO: test all other cases
	// TODO: test timeout

}

func TestHandleUnlocked(t *testing.T) {
	m := getTestWires()

	// straight-forward case, locked triggered while locking
	m.sensors.locked.(*test.PinMock).TheValue = true
	m.sensors.unlocked.(*test.PinMock).TheValue = false
	m.controls.enable.(*test.PinMock).TheValue = true
	state := stateMonitor(m)
	m.reset()
	m.state.Latch = Unlocking
	m.handleSenseUnlocked()
	if state.Latch != Unlocked {
		t.Errorf("want state.Latch=(%v), have (%v)", Unlocked, state.Latch)
	}

}

func TestHandleDoor(t *testing.T) {
	m := getTestWires()
	state := stateMonitor(m)
	m.reset()

	m.sensors.door.(*test.PinMock).TheValue = false
	m.handleSenseDoor()
	if m.state.Door != Open {
		t.Errorf("want state.Door=(%v), have (%v)", Closed, state.Door)
	}

	m.sensors.door.(*test.PinMock).TheValue = true
	m.handleSenseDoor()
	if m.state.Door != Closed {
		t.Errorf("want state.Door=(%v), have (%v)", Open, state.Door)
	}
}

func TestRequestLock(t *testing.T) {
	m := getTestWires()
	// state := stateMonitor(m)
	m.reset()

	// While unlocked and door closed
	m.state.Latch = Unlocked
	m.state.Door = Closed
	newState, err := m.requestLock()
	if err != nil || newState.Latch != Locking {
		t.Errorf("requestLock: state=%#v, err=%#v", newState, err)
	}
}
