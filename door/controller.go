package door

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/kzyapkov/gpio"
	"github.com/kzyapkov/pesho/util"
)

var (
	ErrDoorOpen   = errors.New("door is open")
	ErrNotHandled = errors.New("don't know how to handle this signal")
)

type controller struct {
	*sensors
	*controls

	latchMoveTime time.Duration // time for the motor to be on

	startedAt time.Time

	// where to send state changes
	delegate stateListener

	// serialize access to the state
	mutex *sync.Mutex
	state State
	motor *time.Timer // don't grind the gears!
}

func (w *controller) startMotorTimer() {
	if w.motor != nil {
		w.cancelMotorTimer()
	}
	w.motor = time.AfterFunc(w.latchMoveTime, w.onMotorTimeout)
	w.startedAt = time.Now()
}

func (w *controller) onMotorTimeout() {
	w.stopMotor() // grind gears. not. wait for mutex. not?

	w.mutex.Lock()
	defer w.mutex.Unlock()

	var state State
	w.updateLatchState(&state)
	log.Printf("onMotorTimeout: state %s, found: %s", w.state, state)
	if state.Latch != w.state.Latch {
		w.state.Latch = state.Latch
		w.onState()
	}
}

func (w *controller) cancelMotorTimer() {
	if w.motor != nil {
		w.motor.Stop()
		w.motor = nil
	}
}

func (w *controller) onLocked() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	var state State
	w.updateLatchState(&state)
	log.Printf("onLocked: found %s", w.state.Latch)
	switch w.state.Latch {
	case Locking:
		// we're in flight. ignore, wait for the motor stop timer
		// to trigger an update
		log.Print("Ignoring signal, will wait for the timer to update us")
		return
	default:
		if state.Latch != w.state.Latch {
			w.state.Latch = state.Latch
			w.onState()
		}
	}
}

func (w *controller) onUnlocked() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	var state State
	w.updateLatchState(&state)
	log.Printf("onUnlocked: found %s", state.Latch)
	switch w.state.Latch {
	case Unlocking:
		log.Print("Ignoring signal, will wait for the timer to update us")
		// we're in flight. ignore, wait for the motor stop timer
		// to trigger an update
		return
	default:
		if state.Latch != w.state.Latch {
			w.state.Latch = state.Latch
			w.onState()
		}
	}
}

func (w *controller) onDoorChange() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	var new State
	w.updateDoorState(&new)
	if new.Door != w.state.Door {
		w.state.Door = new.Door
		w.onState()
	}
}

func (w *controller) requestLock() (State, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	log.Printf("requestLock: %v", w.state)

	switch w.state.Latch {
	case Locking:
	case Locked:
		// nothing to do
		return w.state, nil
	case Unlocking:
		// we are currently unlocking. stop.
		w.stopMotor()
		w.cancelMotorTimer()
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
		w.startMotorTimer()
		w.startLocking()
		w.onState()
		return w.state, nil

	default:
		return w.state, ErrNotHandled
	}
	return w.state, ErrInternal
}

func (w *controller) requestUnlock() (State, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	log.Printf("requestUnlock: %v", w.state)

	switch w.state.Latch {
	case Unlocking:
	case Unlocked:
		// nothing to do
		return w.state, nil
	case Locking:
		w.stopMotor()
		w.cancelMotorTimer()
		w.updateLatchState(&w.state)
		fallthrough
	case Locked, Unknown: // for safety, try unlocking
		// unlocking can happen even if the door is open.
		// one may argue it should happen without an external request
		w.state.Latch = Unlocking
		w.startMotorTimer()
		w.startUnlocking()
		w.onState()
		return w.state, nil
	default:
		return w.state, ErrNotHandled
	}
	return w.state, ErrInternal
}

// setDelegate assigns the function to be called when state changes occur.
// delegate must not block.
func (w *controller) setDelegate(delegate stateListener) {
	w.delegate = delegate
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.onState()
}

// onState should only be called from within GPIO callbacks
// and while w.mutex is held. Delegate should do it's thing
// as quickly as possible.
func (w *controller) onState() {
	if w.delegate == nil {
		return
	}
	w.delegate(w.state)
}

// Initialize from sensor state
func (w *controller) reset() {
	w.stopMotor() // just in case
	w.mutex.Lock()
	defer w.mutex.Unlock()

	state := State{}
	w.updateDoorState(&state)
	w.updateLatchState(&state)

	if state.Same(&w.state) {
		return
		log.Printf("reset: same state %s", state.String())
	}
	log.Printf("reset: %s -> %s ", w.state.String(), state.String())
	w.state = state
	w.onState()
}

type stateListener func(State)
type doorAutomata interface {
	setDelegate(stateListener) // function to be called on new state
	onDoorChange()
	onLocked()
	onUnlocked()
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

// updateLatchState doesn't know about transitions
func (s *sensors) updateLatchState(state *State) {
	isLocked := !s.locked.Get()
	isUnlocked := !s.unlocked.Get()
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
	isClosed := !s.door.Get()
	if isClosed {
		state.Door = Closed
	} else {
		state.Door = Open
	}
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

func newController(
	lockedN, unlockedN, doorN, enableN, lockN, unlockN int,
	latchMoveTime int, delegate stateListener) (m doorAutomata, err error) {

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
		if e := recover(); e != nil {
			err = savedError
			log.Printf("recovered %v", e)
			for _, p := range openPins {
				if p == nil {
					return
				}
				p.Close()
			}
		}
	}()

	w := &controller{
		sensors: &sensors{
			locked:   util.Debounced(mustPin(lockedN, gpio.ModeInput), 10*time.Millisecond),
			unlocked: util.Debounced(mustPin(unlockedN, gpio.ModeInput), 10*time.Millisecond),
			door:     util.Debounced(mustPin(doorN, gpio.ModeInput), 10*time.Millisecond),
		},
		controls: &controls{
			enable: mustPin(enableN, gpio.ModeLow),
			lock:   mustPin(lockN, gpio.ModeLow),
			unlock: mustPin(unlockN, gpio.ModeLow),
		},
		mutex:         &sync.Mutex{},
		delegate:      delegate,
		latchMoveTime: time.Duration(latchMoveTime) * time.Millisecond,
	}
	w.reset()
	if err = w.installCallbacks(); err != nil {
		savedError = err
		panic(err)
	}
	return w, nil
}

func (c *controller) installCallbacks() error {
	b := &util.ErrBatch{}
	// only looking at locked now, no point in following both
	b.Do(func() error {
		return c.locked.BeginWatch(gpio.EdgeBoth, c.onLocked)
	})
	// b.do(func() error {
	// 	return c.unlocked.BeginWatch(gpio.EdgeFalling, c.onUnlocked)
	// })
	b.Do(func() error {
		return c.door.BeginWatch(gpio.EdgeFalling, c.onDoorChange)
	})
	return b.Err
}

func (w *controller) close() error {
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
