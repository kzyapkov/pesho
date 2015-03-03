// Package door implements the interface to an actual door lock and its
// associated sensors, controls and logic.
package door

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/kzyapkov/gpio"
	"github.com/kzyapkov/pesho/config"
	"github.com/kzyapkov/pesho/util"
)

var (
	ErrNotSubscribed = errors.New("not currently subscribed")
	ErrInterrupted   = errors.New("the requested door operation was interrupted")
	ErrBusy          = errors.New("Pesho is busy doing something important")
)

type Door struct {
	sensors  *sensors
	controls *controls

	state struct {
		*State
		*sync.Mutex
	}

	// to kill a current Lock/Unlock
	killCurrent chan chan struct{}
	dying       chan struct{}

	subsMutex *sync.Mutex
	subs      *sub
	events    chan DoorEvent

	// State() State                                // The current state of the door
	// Lock() error                                 // Lock, blocks for outcome
	// Unlock() error                               // Unlock, blocks for outcome
	// Close() error                                // Cleanup resources
	// Subscribe(chan *DoorEvent) <-chan *DoorEvent // Notifies all for each new state
	// Unsubscribe(<-chan *DoorEvent) error         // Channel will get no more notifications of events
}

type State struct {
	Latch LatchState `json:"latch"` // Position of the latch (or bolt): locked (extended), unlocked (retracted) or transitioning
	Door  DoorState  `json:"door"`  // Reed switch readout
}

func (s *State) String() string {
	return "State{Door: " + s.Door.String() + ", Latch: " + s.Latch.String() + "}"
}

func (s *State) IsOpen() bool {
	return s.Door == Open
}

func (s *State) IsClosed() bool {
	return s.Door == Closed
}

func (s *State) IsLocked() bool {
	return s.Latch == Locked
}

func (s *State) IsUnlocked() bool {
	return s.Latch == Unlocked
}

func (s *State) IsInFlight() bool {
	switch s.Latch {
	case Locking:
	case Unlocking:
		return true
	default:
	}
	return false
}

func (s *State) Same(other *State) bool {
	return (s.Latch == other.Latch && s.Door == other.Door)
}

type LatchState int

const (
	Locked LatchState = iota
	Unlocked
	Locking
	Unlocking
	Unknown
)

const (
	DoorHWEvent int = iota
	LockHWEvent
)

func (l LatchState) String() string {
	switch l {
	case Locked:
		return "Locked"
	case Unlocked:
		return "Unlocked"
	case Locking:
		return "Locking"
	case Unlocking:
		return "Unlocking"
	case Unknown:
		return "Unknown"
	default:
		return "**FIXME**"
	}
}

func (l LatchState) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strings.ToLower(l.String()) + `"`), nil
}

type DoorState int

const (
	Open DoorState = iota
	Closed
)

func (d DoorState) String() string {
	switch d {
	case Closed:
		return "Closed"
	case Open:
		return "Open"
	default:
		return "**FIXME**"
	}
}

func (d DoorState) MarshalJSON() ([]byte, error) {
	return []byte((`"` + strings.ToLower(d.String()) + `"`)), nil
}

type DoorEvent struct {
	Old, New State
	When     time.Time
}

func (d *Door) State() State {
	d.state.Lock()
	defer d.state.Unlock()
	return *d.state.State
}

func (d *Door) Lock() error {
	return d.moveLatch(Locked)
}

func (d *Door) Unlock() error {
	return d.moveLatch(Unlocked)
}

func (d *Door) monitorPins() {
	toggle := make(chan int)
	d.sensors.door.BeginWatch(gpio.EdgeBoth, func() {
		toggle <- DoorHWEvent
	})
	d.sensors.locked.BeginWatch(gpio.EdgeBoth, func() {
		toggle <- LockHWEvent
	})
	d.sensors.unlocked.BeginWatch(gpio.EdgeBoth, func() {
		toggle <- LockHWEvent
	})

	for {
		select {
		case <-d.dying:
			return
		case <-toggle:
			log.Printf("monitorPins: door=%s; locked=%s; unlocked=%s",
				d.sensors.door.Get(), d.sensors.locked.Get(), d.sensors.unlocked.Get())

			if d.state.IsInFlight() {
				continue
			}
			var newState State
			d.sensors.fetchDoorState(&newState)
			d.sensors.fetchLatchState(&newState)
			// notify() will update d.state
			d.notify(newState)
		}
	}
}

func (d *Door) moveLatch(target LatchState) error {
	d.state.Lock()
	defer d.state.Unlock()

	if d.state.Door == Open {
		log.Print("movelLatch: Door is open")
		if target != Unlocked {
			log.Print("movelLatch: Door is open, won't try locking.")
			return ErrBusy
		}
		log.Print("moveLatch: Door is open, but let's unlock anyway...")
	}

	// Handle no-op cases
	switch d.state.Latch {
	case target:
		log.Printf("movelLatch: already %s", d.state.Latch.String())
		// already there
		return nil

	case Locking:
	case Unlocking:
		log.Printf("moveLatch: now busy with %s, ErrBusy-ing out", d.state.Latch.String())
		// TODO: maybe handle some of these better
		return ErrBusy
	case Unknown:
		log.Printf("moveLatch: state is unknown, but we are brave.")
	}

	var inFlight LatchState
	if target == Locked {
		inFlight = Locking
		d.controls.startLocking()
	} else {
		inFlight = Unlocking
		d.controls.startUnlocking()
	}
	log.Printf("moveLatch: now %s", inFlight.String())
	d.state.Latch = inFlight
	d.killCurrent = make(chan chan struct{})
	d.state.Unlock()

	var ret error
	<-time.After(200 * time.Millisecond)
	d.controls.stopMotor()
	<-time.After(300 * time.Millisecond)

	var newState State

	d.sensors.fetchDoorState(&newState)
	d.sensors.fetchLatchState(&newState)

	d.state.Lock()

	if (newState.IsLocked() && target == Locked) || (!newState.IsLocked() && target == Unlocked) {
		ret = nil
	} else {
		ret = ErrBusy
	}

	d.state.Latch = newState.Latch
	d.state.Door = newState.Door

	log.Print("moveLatch: all is good")
	return ret
}

func (d *Door) notify(new State) {
	go func() {
		d.state.Lock()
		defer d.state.Unlock()

		old := *d.state.State
		if old.Same(&new) {
			return
		}
		d.state.State = &new
		var evt *DoorEvent = &DoorEvent{old, new, time.Now()}

		d.subsMutex.Lock()
		defer d.subsMutex.Unlock()
		s := d.subs
		for s != nil {
			if cap(s.c) > 0 {
				s.c <- evt
			} else {
				log.Printf("skip notifying %v, no capacity", s)
			}
			s = s.next
		}
	}()
}

type sub struct {
	c    chan *DoorEvent
	next *sub // linked list in door
}

func (d *Door) Subscribe(c chan *DoorEvent) <-chan *DoorEvent {
	if c == nil {
		c = make(chan *DoorEvent, 3)
	}

	newSub := &sub{c: c}

	d.subsMutex.Lock()
	defer d.subsMutex.Unlock()
	newSub.next = d.subs
	d.subs = newSub

	return c
}

func (d *Door) Unsubscribe(c <-chan *DoorEvent) error {
	var s, prev *sub
	s = d.subs

	d.subsMutex.Lock()
	defer d.subsMutex.Unlock()

	for s != nil {
		// find the subscription
		if s.c != c {
			prev, s = s, s.next
			continue
		}

		// remove it from the linked list
		if prev != nil {
			prev.next = s.next
		} else {
			// it's is the first item
			d.subs = s.next
		}
		return nil
	}
	return ErrNotSubscribed
}

func (d *Door) Close() error {
	close(d.dying)
	// verify cleanup!
	return nil
}

// GPIO inputs
type sensors struct {
	locked   gpio.Pin
	unlocked gpio.Pin
	door     gpio.Pin
}

// fetchLatchState doesn't know about transitions
func (s *sensors) fetchLatchState(state *State) {
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

func (s *sensors) fetchDoorState(state *State) {
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

func NewFromConfig(cfg config.DoorConfig) (d *Door, err error) {

	// can you do it right in less lines?
	// begin pin-opening boilerplate
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
	// end pin-opening boilerplate

	d = &Door{
		sensors: &sensors{
			locked:   util.Debounced(mustPin(cfg.Pins.SenseLocked, gpio.ModeInput), 10*time.Millisecond),
			unlocked: util.Debounced(mustPin(cfg.Pins.SenseUnlocked, gpio.ModeInput), 10*time.Millisecond),
			door:     util.Debounced(mustPin(cfg.Pins.SenseDoor, gpio.ModeInput), 10*time.Millisecond),
		},
		controls: &controls{
			enable: mustPin(cfg.Pins.LatchEnable, gpio.ModeLow),
			lock:   mustPin(cfg.Pins.LatchLock, gpio.ModeLow),
			unlock: mustPin(cfg.Pins.LatchUnlock, gpio.ModeLow),
		},
		subsMutex: &sync.Mutex{},
	}

	d.state.State = &State{}
	d.state.Mutex = &sync.Mutex{}
	go d.monitorPins()

	var theState State
	d.sensors.fetchDoorState(&theState)
	d.sensors.fetchLatchState(&theState)
	d.notify(theState)

	return d, nil
}
