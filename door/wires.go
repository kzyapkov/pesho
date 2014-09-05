package door

import (
	"errors"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/kzyapkov/gpio"
)

var (
	ErrDoorOpen   = errors.New("door is open")
	ErrNotHandled = errors.New("don't know how to handle this signal229")
)

type stateListener func(State)
type doorAutomata interface {
	setDelegate(stateListener)     // function to be called on new state
	handleSenseLocked()            // called on the falling edge of the locked pin
	handleSenseUnlocked()          // called on the falling edge of the unlocked pin
	handleSenseDoor()              // called on the
	requestLock() (State, error)   // called by clients wanting to get out of this place
	requestUnlock() (State, error) // called by villans wanting to keep victims in
	reset()                        // may be called any number of times to re-init the state from the inputs
	close() error                  // must only be called once
}

// GPIO inputs
type sensors struct {
	locked   gpio.Pin
	unlocked gpio.Pin
	door     gpio.Pin // high means closed
}

func readPin(pin gpio.Pin, invert bool) bool {
	value := pin.Get()
	// neat xor trick
	return value != invert
}

func (s *sensors) readLocked() bool {
	return readPin(s.locked, false)
}

func (s *sensors) readUnlocked() bool {
	return readPin(s.unlocked, false)
}

func (s *sensors) readDoor() bool {
	return readPin(s.door, false)
}

// updateLatchState doesn't know about transitions
func (s *sensors) updateLatchState(state *State) {
	isLocked := s.readLocked()
	isUnlocked := s.readUnlocked()
	if isLocked == isUnlocked {
		state.Latch = Unknown
	} else {
		if isLocked {
			state.Latch = Locked
		} else {
			state.Latch = Unlocked
		}
	}
}

func (s *sensors) updateDoorState(state *State) {
	isClosed := s.readDoor()
	if isClosed {
		state.Door = Closed
	} else {
		state.Door = Open
	}
}

func (s *sensors) updateState(state *State) {
	s.updateLatchState(state)
	s.updateDoorState(state)
}

// GPIO outputs
type controls struct {
	enable gpio.Pin
	lock   gpio.Pin
	unlock gpio.Pin
}

func (c *controls) startLocking() {
	c.unlock.Clear()
	c.lock.Set()
	c.enable.Set()
}

func (c *controls) startUnlocking() {
	c.lock.Clear()
	c.unlock.Set()
	c.enable.Set()
}

func (c *controls) stopMotor() {
	c.enable.Clear()
	c.lock.Clear()
	c.unlock.Clear()
}

type wires struct {
	*sensors
	*controls

	// maximum time for the motor to be on
	maxMotorRuntime time.Duration

	// where to send state changes
	delegate stateListener

	// serialize access to the state
	mutex *sync.Mutex
	state State
	timer *time.Timer // don't grind the gears!
}

func (w *wires) cancelLatchTimer() {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *wires) startLatchTimer() {
	if w.timer != nil {
		w.cancelLatchTimer()
	}
	w.timer = time.AfterFunc(w.maxMotorRuntime, w.latchTimeout)
}

func (w *wires) handleSenseLocked() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	switch w.state.Latch {
	case Locking:
		// we're done locking
		w.stopMotor()
		w.state.Latch = Locked
		w.onState()
	case Unknown:
		var new State
		w.updateLatchState(&new)
		if new.Latch != Unknown {
			w.state.Latch = new.Latch
			w.onState()
		}
	default:
		log.Printf("%#v, ignoring lock event", w.state)
	}
}

func (w *wires) handleSenseUnlocked() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	switch w.state.Latch {
	case Unlocking:
		// we're done unlocking
		w.stopMotor()
		w.cancelLatchTimer()
		w.state.Latch = Unlocked
		w.onState()
	case Unknown:
		var new State
		w.updateLatchState(&new)
		if new.Latch != Unknown {
			w.state.Latch = new.Latch
			w.onState()
		}
	default:
		log.Printf("%v, ignoring unlock event", w.state)
	}
}

func (w *wires) handleSenseDoor() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	var new State
	w.updateDoorState(&new)
	if new.Door != w.state.Door {
		w.state.Door = new.Door
		w.onState()
	}
}

func (w *wires) requestLock() (State, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	switch w.state.Latch {
	case Locking:
	case Locked:
		// nothing to do
		return w.state, nil
	case Unlocking:
		// we are currently unlocking. stop.
		w.stopMotor()
		w.cancelLatchTimer()
		// we're somewhere midway. try to set a proper state
		// because the door sensor may prevent startLocking()
		w.updateLatchState(&w.state)
		// and now, begin locking
		fallthrough
	case Unlocked:
		// only if the door is closed
		if w.state.Door != Closed {
			return w.state, ErrDoorOpen
		}
		w.state.Latch = Locking
		w.startLatchTimer()
		w.startLocking()
		w.onState()
		return w.state, nil

	default:
	}
	return w.state, ErrNotHandled
}

func (w *wires) requestUnlock() (State, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	switch w.state.Latch {
	case Unlocking:
	case Unlocked:
		// nothing to do
		return w.state, nil
	case Locking:
		w.stopMotor()
		w.cancelLatchTimer()
		w.updateLatchState(&w.state)
		fallthrough
	case Locked:
		// unlocking can happen even if the door is open.
		// one may argue it should happen without an external request
		w.state.Latch = Unlocking
		w.startLatchTimer()
		w.startUnlocking()
		w.onState()
		return w.state, nil
	}
	return State{}, ErrNotHandled
}

func (w *wires) latchTimeout() {
	w.stopMotor() // grind gears. not. wait for mutex. not?

	w.mutex.Lock()
	defer w.mutex.Unlock()

	var state State
	w.updateLatchState(&state)
	if state.Latch != w.state.Latch {
		w.state.Latch = state.Latch
		w.onState()
	}
}

// setDelegate assigns the function to be called when state changes occur.
// delegate must not block.
func (w *wires) setDelegate(delegate stateListener) {
	w.delegate = delegate
	w.reset()
}

// onState should only be called from within GPIO callbacks
// and while w.mutex is held. Delegate should do it's thing
// as quickly as possible.
func (w *wires) onState() {
	if w.delegate == nil {
		return
	}
	w.delegate(w.state)
}

// Initialize from sensor state
func (w *wires) reset() {
	w.stopMotor() // just in case
	w.mutex.Lock()
	defer w.mutex.Unlock()

	state := State{}
	w.updateState(&state)

	if state.Same(&w.state) {
		return
		log.Printf("reset: same state %#v", state)
	}
	log.Printf("reset: %#v -> %#v ", w.state, state)
	w.state = state
	w.onState()
}

func wireUp(lockedN, unlockedN, doorN, enableN, lockN, unlockN int,
	maxMotorRuntimeMs int, delegate stateListener) (m doorAutomata, err error) {

	openPins := make([]gpio.Pin, 6)
	var savedError error

	mustPin := func(n int, mode gpio.Mode) gpio.Pin {
		if pin, err := gpio.OpenPin(n, mode); err != nil {
			savedError = err
			panic(err)
		} else {
			openPins[len(openPins)] = pin
			return pin
		}
	}

	defer func() {
		if n := recover(); n != nil {
			if _, ok := n.(runtime.Error); ok {
				panic(n)
			}
			for _, p := range openPins {
				p.Close()
				err = savedError
			}
		}
	}()

	w := &wires{
		sensors: &sensors{
			locked:   mustPin(lockedN, gpio.ModeInput),
			unlocked: mustPin(unlockedN, gpio.ModeInput),
			door:     mustPin(doorN, gpio.ModeInput),
		},
		controls: &controls{
			enable: mustPin(enableN, gpio.ModeLow),
			lock:   mustPin(lockN, gpio.ModeLow),
			unlock: mustPin(unlockN, gpio.ModeLow),
		},
		mutex:           &sync.Mutex{},
		delegate:        delegate,
		maxMotorRuntime: time.Duration(maxMotorRuntimeMs) * time.Millisecond,
	}
	w.reset()
	if err = w.callbacks(); err != nil {
		savedError = err
		panic(err)
	}
	return w, nil
}

type batchwatch struct {
	err error
}

func (w batchwatch) watch(pin gpio.Pin, edge gpio.Edge, f gpio.IRQEvent) {
	if w.err != nil {
		return
	}
	err := pin.BeginWatch(edge, f)
	if err != nil {
		w.err = err
	}
}

func (w *wires) callbacks() error {
	var wb batchwatch
	wb.watch(w.locked, gpio.EdgeFalling, w.handleSenseLocked)
	wb.watch(w.unlocked, gpio.EdgeFalling, w.handleSenseUnlocked)
	wb.watch(w.door, gpio.EdgeBoth, w.handleSenseDoor)
	return wb.err
}

func (w *wires) close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	var err error
	for _, pin := range []gpio.Pin{
		w.controls.enable, w.controls.lock, w.controls.unlock,
		w.sensors.locked, w.sensors.unlocked, w.sensors.door,
	} {
		e := pin.Close()
		if e != nil {
			err = e
		}
	}
	return err
}
