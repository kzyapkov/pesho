package door

import (
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/kzyapkov/gpio"
)

type stateListener func(State)
type doorAutomata interface {
	stateDelegate(stateListener)
	handleSenseLocked()
	handleSenseUnlocked()
	handleSenseDoor()
	requestLock() (State, error)
	requestUnlock() (State, error)
	reset()
}

type wires struct {
	// GPIOs, outputs
	locked   gpio.Pin
	unlocked gpio.Pin
	door     gpio.Pin // high means closed
	// ... inputs
	enable gpio.Pin
	lock   gpio.Pin
	unlock gpio.Pin

	// where to send state changes
	delegate stateListener

	// serialize access to the state
	*sync.Mutex
	state State
	timer time.Timer
}

func (w *wires) stateDelegate(delegate stateListener) {
	w.delegate = delegate
	w.reset()
}

// onState should only be called from within GPIO callbacks
// and while the mutex is being held. It launches a new goroutine
// for the delegate to handle the state event.
func (w *wires) onState() {
	if w.delegate == nil {
		return
	}
	d := w.delegate
	state := w.state
	go func() { d(state) }()
}

// Initialize from sensor state
func (w *wires) reset() {
	state := w.findState()
	w.Lock()
	defer w.Unlock()

	if state.Same(&w.state) {
		return
	}
	w.state = *state
	w.onState()
}

func (w *wires) findState() *State {
	var state State
	state.Latch = w.findLatchState()
	state.Door = w.findDoorState()
	return &state
}

func (w *wires) findDoorState() DoorState {
	isClosed := w.door.Get()
	var ds DoorState
	if isClosed {
		ds = Closed
	} else {
		ds = Open
	}
	return ds
}

func (w *wires) findLatchState() LatchState {
	isLocked := w.locked.Get()
	isUnlocked := w.unlocked.Get()
	var ls LatchState
	if isLocked == isUnlocked {
		ls = Unknown
	} else {
		if isLocked {
			ls = Locked
		} else {
			ls = Unlocked
		}
	}
	return ls
}

func connectPins(lockedN, unlockedN, doorN, enableN, lockN, unlockN int, delegate stateListener) (m doorAutomata, err error) {

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
		locked:   mustPin(lockedN, gpio.ModeInput),
		unlocked: mustPin(unlockedN, gpio.ModeInput),
		door:     mustPin(doorN, gpio.ModeInput),
		enable:   mustPin(enableN, gpio.ModeLow),
		lock:     mustPin(lockN, gpio.ModeLow),
		unlock:   mustPin(unlockN, gpio.ModeLow),
		delegate: delegate,
		Mutex:    &sync.Mutex{},
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

func (w *wires) handleSenseLocked() {
	w.Lock()
	defer w.Unlock()

	switch w.state.Latch {
	case Locking:
		// we're done locking
		w.enable.Clear()
		w.lock.Clear()
		w.unlock.Clear()

		w.state.Latch = Locked
		w.onState()
	case Unknown:
		new := w.findLatchState()
		if new != Unknown {
			w.state.Latch = new
			w.onState()
		}
	default:
		log.Printf("%v, ignoring lock event", w.state)
	}
}
func (w *wires) handleSenseUnlocked() {
	w.Lock()
	defer w.Unlock()

	switch w.state.Latch {
	case Unlocking:
		// we're done unlocking
		w.enable.Clear()
		w.lock.Clear()
		w.unlock.Clear()

		w.state.Latch = Unlocked
		w.onState()
	case Unknown:
		new := w.findLatchState()
		if new != Unknown {
			w.state.Latch = new
			w.onState()
		}
	default:
		log.Printf("%v, ignoring unlock event", w.state)
	}
}

func (w *wires) handleSenseDoor() {
	w.Lock()
	defer w.Unlock()
	door := w.findDoorState()
	if door != w.state.Door {
		w.state.Door = door
		w.onState()
	}
}

func (w *wires) requestLock() (State, error) {
	return State{}, nil
}

func (w *wires) requestUnlock() (State, error) {
	return State{}, nil
}

func (w *wires) latchTimeout() {

}
