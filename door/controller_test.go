package door

import (
	"sync"
	"testing"

	"github.com/kzyapkov/gpio/test"
)

func getTestController() *controller {
	w := &controller{
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

func stateMonitor(w *controller) *State {
	var state State
	w.setDelegate(func(s State) {
		state = s
	})
	return &state
}

func TestGettingTestWires(t *testing.T) {
	var m doorAutomata
	m = getTestController()
	m.setDelegate(func(s State) {})
}

func TestSensorsReadDoor(t *testing.T) {
	c := sensors{
		&test.PinMock{},
		&test.PinMock{},
		&test.PinMock{},
	}

	c.door.(*test.PinMock).TheValue = false
	state := &State{}
	c.updateDoorState(state)
	if state.Door != Closed {
		t.Fatal("door should be closed")
	}
}

func TestInitialLatchStates(t *testing.T) {
	m := getTestController()
	m.sensors.locked.(*test.PinMock).TheValue = false
	m.sensors.unlocked.(*test.PinMock).TheValue = true
	state := stateMonitor(m)

	m.reset()
	if state.Latch != Locked {
		t.Errorf("want state.Latch=(%v), have (%v)", Locked, state.Latch)
	}

	m.sensors.locked.(*test.PinMock).TheValue = true
	m.sensors.unlocked.(*test.PinMock).TheValue = false

	m.reset()
	if state.Latch != Unlocked {
		t.Errorf("latch should be locked")
	}

}

func TestInitialStateInvalid(t *testing.T) {
	m := getTestController()
	m.locked.(*test.PinMock).TheValue = false
	m.unlocked.(*test.PinMock).TheValue = false
	m.door.(*test.PinMock).TheValue = true
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
	m := getTestController()
	m.door.(*test.PinMock).TheValue = true
	state := stateMonitor(m)
	m.reset()
	if state.Door != Open {
		t.Fail()
	}
	m.door.(*test.PinMock).TheValue = false
	m.reset()
	if state.Door != Closed {
		t.Fail()
	}
}

func TestHandleLocked(t *testing.T) {
	m := getTestController()

	// straight-forward case, locked triggered while locking
	m.sensors.locked.(*test.PinMock).TheValue = false
	m.sensors.unlocked.(*test.PinMock).TheValue = true
	m.controls.enable.(*test.PinMock).TheValue = true
	state := stateMonitor(m)
	m.reset()
	m.state.Latch = Locking
	m.sensors.locked.(*test.PinMock).TheValue = true
	m.sensors.unlocked.(*test.PinMock).TheValue = false
	m.onLocked()
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
	m := getTestController()

	// straight-forward case, locked triggered while locking
	m.sensors.locked.(*test.PinMock).TheValue = true
	m.sensors.unlocked.(*test.PinMock).TheValue = false
	m.controls.enable.(*test.PinMock).TheValue = true
	state := stateMonitor(m)
	m.reset()
	m.state.Latch = Unlocking
	m.sensors.locked.(*test.PinMock).TheValue = false
	m.sensors.unlocked.(*test.PinMock).TheValue = true
	m.onUnlocked()
	if state.Latch != Unlocked {
		t.Errorf("want state.Latch=(%v), have (%v)", Unlocked, state.Latch)
	}
}

func TestHandleDoor(t *testing.T) {
	m := getTestController()
	state := stateMonitor(m)
	m.reset()

	m.sensors.door.(*test.PinMock).TheValue = true
	m.onDoorChange()
	if m.state.Door != Open {
		t.Errorf("want state.Door=(%v), have (%v)", Open, state.Door)
	}

	m.sensors.door.(*test.PinMock).TheValue = false
	m.onDoorChange()
	if m.state.Door != Closed {
		t.Errorf("want state.Door=(%v), have (%v)", Closed, state.Door)
	}
}

func TestRequestLock(t *testing.T) {
	m := getTestController()
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
