// Package implements the interface to an actual door lock and its
// associated sensors, controls and logic.
package door

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/kzyapkov/pesho/config"
)

var (
	ErrNotSubscribed = errors.New("not currently subscribed")
	ErrTimeout       = errors.New("Internal operation timeout")
	ErrInternal      = errors.New("Internal machinery error")
	ErrNotDone       = errors.New("the requested door operation was not completed")
)

// A door object
type Door interface {
	State() State                                // The current state of the door
	Lock() error                                 // Lock, blocks for outcome
	Unlock() error                               // Unlock, blocks for outcome
	Close() error                                // Cleanup resources
	Subscribe(chan *DoorEvent) <-chan *DoorEvent // Notifies all for each new state
	Unsubscribe(<-chan *DoorEvent) error         // Channel will get no more notifications of events
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

type door struct {
	machine doorAutomata
	state   struct {
		*State
		*sync.Mutex
	}

	subsMutex *sync.Mutex
	subs      *sub
	events    chan DoorEvent
}

func (d *door) State() State {
	d.state.Lock()
	defer d.state.Unlock()
	return *d.state.State
}

func (d *door) Lock() error {
	state, err := d.machine.requestLock()
	if err != nil {
		return err
	}
	if state.Latch == Locked {
		return nil // All good, we're locked
	}
	if state.Latch != Locking {
		log.Printf("while locking, state=(%#v) but no error?", state)
		return ErrInternal
	}
	updates := d.Subscribe(nil)
	defer d.Unsubscribe(updates)
	for {
		select {
		case evt := <-updates:
			state = evt.New
			if state.Latch == Locked {
				return nil
			}
			if state.Latch != Locking {
				return ErrNotDone
			}
		case <-time.After(1 * time.Second):
			return ErrTimeout
		}
	}
	log.Panic("this line should never be reached")
	return ErrInternal
}
func (d *door) Unlock() error {
	state, err := d.machine.requestUnlock()
	if err != nil {
		return err
	}
	if state.Latch == Unlocked {
		return nil
	}
	if state.Latch != Unlocking {
		log.Printf(" *** THIS IS BAD *** while unlocking, state=(%#v) but no error?", state)
		return ErrInternal
	}
	updates := d.Subscribe(nil)
	defer d.Unsubscribe(updates)
	for {
		select {
		case evt := <-updates:
			state = evt.New
			if state.Latch == Unlocked {
				return nil
			}
			if state.Latch != Unlocking {
				return ErrNotDone
			}
		case <-time.After(1 * time.Second):
			return ErrTimeout
		}
	}
	log.Panic("this line should never be reached")
	return ErrInternal
}

func (d *door) notify(new State) {
	// TODO: implement these notifications
	//       with a single long-living goroutine and a channel
	go func() {
		d.state.Lock()
		defer d.state.Unlock()
		old := *d.state.State
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
	return nil
}

func NewFromConfig(cfg config.DoorConfig) (Door, error) {
	d := &door{
		subsMutex: &sync.Mutex{},
	}
	d.state.State = &State{}
	d.state.Mutex = &sync.Mutex{}
	m, err := newController(
		cfg.Pins.SenseLocked, cfg.Pins.SenseUnlocked, cfg.Pins.SenseDoor,
		cfg.Pins.LatchEnable, cfg.Pins.LatchLock, cfg.Pins.LatchUnlock,
		cfg.LatchMoveTime, d.notify,
	)
	if err != nil {
		return nil, err
	}
	d.machine = m
	return d, nil
}
