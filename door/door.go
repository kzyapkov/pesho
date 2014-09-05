// Package implements the interface to an actual door lock and its
// associated sensors, controls and logic.
package door

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/kzyapkov/pesho/config"
)

var ErrNotSubscribed = errors.New("not currently subscribed")

type State struct {
	Latch LatchState
	Door  DoorState
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

type DoorState int

const (
	Open DoorState = iota
	Closed
)

type DoorEvent struct {
	Old, New State
	When     time.Time
}

// A door object
type Door interface {
	State() State                                // The current state of the door
	Lock() error                                 // Lock, blocks for outcome
	Unlock() error                               // Unlock, blocks for outcome
	Close() error                                // Cleanup resources
	Subscribe(chan *DoorEvent) <-chan *DoorEvent // Notifies all for each new state
	Unsubscribe(<-chan *DoorEvent) error         // Channel will get no more notifications of events
}

type door struct {
	machine doorAutomata
	state   struct {
		*State
		sync.Mutex
	}

	subsMutex *sync.Mutex
	subs      *sub
	events    chan DoorEvent
}

func (d *door) State() State {
	return *d.state.State
}
func (d *door) Lock() error {
	d.machine.requestLock()
	// TODO: wait for completion
	return nil
}
func (d *door) Unlock() error {
	return nil
}

func (d *door) notify(new State) {
	// TODO: get rid of the mutex, implement these notifications
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
	m, err := wireUp(
		cfg.Pins.SenseLocked, cfg.Pins.SenseUnlocked, cfg.Pins.SenseDoor,
		cfg.Pins.LatchEnable, cfg.Pins.LatchLock, cfg.Pins.LatchUnlock,
		cfg.MaxMotorRuntimeMs, d.notify,
	)
	if err != nil {
		return nil, err
	}
	d.machine = m
	return d, nil
}
