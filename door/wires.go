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
	ErrNotHandled = errors.New("don't know how to handle this signal")
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
	door     gpio.Pin
}

func (s *sensors) readLocked() bool {
	return !s.locked.Get() // closed switch (latch engaged) => low level on GPIO
}

func (s *sensors) watchLocked(f func()) error {
	return s.locked.BeginWatch(gpio.EdgeFalling, f)
}

func (s *sensors) readUnlocked() bool {
	return !s.unlocked.Get() // closed switch (latch disengaged) => low level on GPIO
}

func (s *sensors) watchUnlocked(f func()) error {
	return s.unlocked.BeginWatch(gpio.EdgeFalling, f)
}

func (s *sensors) readDoorClosed() bool {
	return !s.door.Get() // switch closed (door closed) => low level on GPIO
}

func (s sensors) watchDoor(f func()) error {
	return s.door.BeginWatch(gpio.EdgeBoth, f)
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
	isClosed := s.readDoorClosed()
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
	c.lock.Set()
	c.enable.Set()
}

func (c *controls) startUnlocking() {
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

	startedAt time.Time

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
	w.startedAt = time.Now()
}

func (w *wires) handleSenseLocked() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	switch w.state.Latch {
	case Locking:
		// we're done locking
		w.stopMotor()
		w.cancelLatchTimer()
		w.state.Latch = Locked
		w.onState()
		lasted := time.Now().Sub(w.startedAt)
		log.Printf("handleSenseLocked: done Locking in %s, %s", lasted, w.state.String())
	case Unknown:
		log.Printf("handleSenseLocked: found unknown, %s", w.state.String())
		var new State
		w.updateLatchState(&new)
		if new.Latch != Unknown {
			log.Printf("handleSenseLocked: recovered to %s", new.String())
			w.state.Latch = new.Latch
			w.onState()
		}
	default:
		log.Printf("handleSenseLocked: ignoring, %s", w.state.String())
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
		lasted := time.Now().Sub(w.startedAt)
		log.Printf("handleSenseUnlocked: done unlocking in %s, %s", lasted, w.state.String())
	case Unknown:
		log.Printf("handleSenseUnlocked: found unknown, %s", w.state.String())
		var new State
		w.updateLatchState(&new)
		if new.Latch != Unknown {
			log.Printf("handleSenseUnlocked: recovered to %s", new.String())
			w.state.Latch = new.Latch
			w.onState()
		}
	default:
		log.Printf("handleSenseUnlocked: ignoring, %s", w.state.String())
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
		if w.state.IsOpen() {
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

	log.Print("latchTimeout triggered")

	w.mutex.Lock()
	defer w.mutex.Unlock()

	var state State
	w.updateLatchState(&state)
	isNew := state.Latch != w.state.Latch
	log.Printf("latchTimeout: state %s, will update: %t", state.String(), isNew)
	if isNew {
		w.state.Latch = state.Latch
		w.onState()
	}
}

// setDelegate assigns the function to be called when state changes occur.
// delegate must not block.
func (w *wires) setDelegate(delegate stateListener) {
	w.delegate = delegate
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.onState()
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
		log.Printf("reset: same state %s", state.String())
	}
	log.Printf("reset: %s -> %s ", w.state.String(), state.String())
	w.state = state
	w.onState()
}

func wireUp(lockedN, unlockedN, doorN, enableN, lockN, unlockN int,
	maxMotorRuntimeMs int, delegate stateListener) (m doorAutomata, err error) {

	openPins := make([]gpio.Pin, 6)
	var i int
	var savedError error

	mustPin := func(n int, mode gpio.Mode) gpio.Pin {
		if pin, err := gpio.OpenPin(n, mode); err != nil {
			savedError = err
			panic(err)
		} else {
			openPins[i] = pin
			i++
			return pin
		}
	}

	defer func() {
		if n := recover(); n != nil {
			if _, ok := n.(runtime.Error); ok {
				panic(n)
			}
			for _, p := range openPins {
				if p == nil {
					return
				}
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

type batch struct {
	err error
}

func (w batch) do(f func(func()) error, cb func()) {
	if w.err != nil {
		return
	}
	err := f(cb)
	if err != nil {
		w.err = err
	}
}

func (w *wires) callbacks() error {
	var b batch
	b.do(w.watchLocked, w.handleSenseLocked)
	b.do(w.watchUnlocked, w.handleSenseUnlocked)
	b.do(w.watchDoor, w.handleSenseDoor)
	return b.err
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
