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
	ErrInternal      = errors.New("Internal machinery error")
	ErrNotDone       = errors.New("the requested door operation was not completed")
	ErrDoorOpen      = errors.New("door is open")
)

//
type Door interface {
	State() State                                // The current state of the door
	Lock() error                                 // Lock, blocks for outcome
	Unlock() error                               // Unlock, blocks for outcome
	Subscribe(chan *DoorEvent) <-chan *DoorEvent // Notifies all for each new state
	Unsubscribe(<-chan *DoorEvent) error         // Channel will get no more notifications of events
	Close() error                                // Cleanup resources
}

type State struct {
	Latch LatchState `json:"latch"`
	Door  DoorState  `json:"door"`
}

func (s *State) String() string {
	return "State{Door: " + s.Door.String() + ", Latch: " + s.Latch.String() + "}"
}

func (s *State) IsOpen() bool {
	return s.Door == Open
}

func (s *State) IsLocked() bool {
	return s.Latch == Locked
}

func (s *State) IsInFlight() bool {
	switch s.Latch {
	case Unlocking, Locking:
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

// GPIO inputs
type sensors struct {
	locked gpio.Pin
	// unlocked gpio.Pin
	door gpio.Pin
}

func (s *sensors) isLocked() {
	return !s.locked.Get()
}

func (s *sensors) isClosed() {
	return !s.door.Get()
}

// GPIO outputs
type controls struct {
	enable gpio.Pin
	lock   gpio.Pin
	unlock gpio.Pin
}

func (c *controls) startLocking() {
	log.Print("startLocking")
	c.lock.Set()
	c.enable.Set()
}

func (c *controls) startUnlocking() {
	log.Print("startUnlocking")
	c.unlock.Set()
	c.enable.Set()
}

func (c *controls) stopMotor() {
	log.Print("stopMotor")
	c.enable.Clear()
	c.lock.Clear()
	c.unlock.Clear()
}

type door struct {
	state struct {
		*State
		*sync.Mutex
	}

	latchMoveTime time.Duration

	in  *sensors
	out *controls

	quitCurrentJob chan struct{}

	subsMutex *sync.Mutex
	subs      *sub
	events    chan DoorEvent

	closing chan struct{}
}

func (d *door) State() State {
	d.state.Lock()
	defer d.state.Unlock()
	return *d.state.State
}

func (d *door) Lock() error {
	d.state.Lock()
	defer d.state.Unlock()

	switch d.state.Latch {
	case Locked:
		return nil
	case Locking:
		break
	case Unlocking:
		close(d.quitCurrentJob)
		fallthrough
	case Unlocked:
		if d.state.Door != Closed {
			return ErrDoorOpen
		}
		d.quitCurrentJob = make(chan struct{})
		go d.dolock()
	default:
		log.Printf("Lock: unknown state...")
	}

	updates := d.Subscribe(nil)
	defer d.Unsubscribe(updates)
	for {
		select {
		case new := <-updates:
			if new.Latch == Locked {
				return nil
			}
			if new.Latch != Locking {
				return ErrNotDone
			}
		case <-time.After(1 * time.Second):
			return ErrInternal
		}
	}
}

func (d *door) Unlock() error {
	d.state.Lock()
	defer d.state.Unlock()

	if d.state.Latch == Unlocked {
		return nil
	}

	switch d.state.Latch {
	case Unlocking:
		break
	case Locking:
		d.out.stopMotor()
		fallthrough
	case Locked:
		d.out.startLocking()
		d.startMotorTimer()
		d.scheduleStateUpdate()
	default:
		log.Printf("Unlock: unknown state...")
	}

	updates := d.Subscribe(nil)
	defer d.Unsubscribe(updates)
	for {
		select {
		case new := <-updates:
			if new.Latch == Unlocked {
				return nil
			}
			if new.Latch != Unlocking {
				return ErrNotDone
			}
		case <-time.After(1 * time.Second):
			return ErrInternal
		}
	}
}

func (d *door) notify(new State) {
	// TODO: implement these notifications
	//       with a single long-living goroutine and a channel
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

func (d *door) Subscribe(c chan *DoorEvent) <-chan *DoorEvent {
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

func (d *door) Unsubscribe(c <-chan *DoorEvent) error {
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

func (d *door) Close() error {
	return d.controller.close()
}

func NewFromConfig(cfg config.DoorConfig) (d *door, err error) {

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

	d = &door{
		latchMoveTime: time.Duration(cfg.LatchMoveTime) * time.Millisecond,
		out: &controls{
			enable: mustPin(cfg.Pins.LatchEnable, gpio.ModeLow),
			lock:   mustPin(cfg.Pins.LatchLock, gpio.ModeLow),
			unlock: mustPin(cfg.Pins.LatchUnlock, gpio.ModeLow),
		},
		in: &sensors{
			locked: util.DebouncedInput(mustPin(cfg.Pins.SenseLocked), 10*time.Millisecond),
			//			unlocked: util.DebouncedInput(mustPin(cfg.Pins.SenseUnlocked), 10*time.Millisecond),
			door: util.DebouncedInput(mustPin(cfg.Pins.SenseDoor), 10*time.Millisecond),
		},
		subsMutex: &sync.Mutex{},
	}
	d.state.State = &State{}
	d.state.Mutex = &sync.Mutex{}

	if err != nil {
		return nil, err
	}
	//	d.controller = m
	return d, nil
}
