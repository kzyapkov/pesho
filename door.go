package main

import (
	"errors"
	"log"
	"sync"

	"time"

	"github.com/kzyapkov/gpio"
)

type DoorConfig struct {
	Pins         PinsConfig
	MaxMotorTime int
}

type Door interface {
	Subscribe(chan DoorState) <-chan DoorState // Returns a channel of sensor events
	Unsubscribe(<-chan DoorState) error        // Channel will get no more notifications of events
	Lock() error                               // Engage the latch, blocking
	Unlock() error                             // Disengage the latch, blocking
	State() DoorState                          // Get the current state
	Close() error                              // Clean up resources
}

var ErrNotSubscribed = errors.New("not currently subscribed")

type DoorState struct {
	Locked   bool      // The state of the mechanical latch, true=locked
	Closed   bool      // Whether the door is closed or ajar
	InFlight bool      // Whether we are currently changing Locked, in which case Locked is the old value
	When     time.Time // When did this event occur
}

type PinsConfig struct {
	// outputs
	LatchEnable int
	LatchLock   int
	LatchUnlock int
	// inputs
	SenseLocked   int
	SenseUnlocked int
	SenseDoor     int
}

type door struct {

	// GPIOs
	// outputs
	latchEnable gpio.Pin
	latchLock   gpio.Pin
	latchUnlock gpio.Pin
	// inputs
	senseLocked   gpio.Pin
	senseUnlocked gpio.Pin
	senseDoor     gpio.Pin

	stateMutex sync.RWMutex
	state      DoorState
	events     chan DoorState
	subsMutex  sync.Mutex
	subs       *sub
	closing    chan bool
}

func NewDoor(cfg DoorConfig) (Door, error) {
	var err error
	d := &door{
		events: make(chan DoorState, 15),
	}
	if err = d.createPins(cfg.Pins); err != nil {
		return nil, err
	}

	d.installCallbacks()
	go d.watch()

	return d, nil
}

func (d *door) installCallbacks() {
	d.senseLocked.BeginWatch(gpio.EdgeBoth, d.onSenseLocked)
	d.senseUnlocked.BeginWatch(gpio.EdgeBoth, d.onSenseUnlocked)
	d.senseDoor.BeginWatch(gpio.EdgeBoth, d.onSenseDoor)
}

func (d *door) updateState(locked, closed *bool) *DoorState {
	d.stateMutex.Lock()
	defer d.stateMutex.Unlock()
	prev := &DoorState{}
	*prev = d.state
	mutated := false
	if locked != nil {
		if d.state.Locked != *locked {
			d.state.Locked = *locked
			mutated = true
		} else {
			log.Printf("signal for locked, but no change, ignoring %t", *locked)
		}
	}
	if closed != nil {
		if d.state.Closed != *closed {
			d.state.Closed = *closed
			mutated = true
		} else {
			log.Printf("signal for closed, but no change, ignoring %t", *closed)
		}
	}
	if mutated {
		d.state.When = time.Now()
		d.events <- d.state
		return prev
	}
	return nil
}

func (d *door) onSenseLocked() {
	isLocked := d.senseLocked.Get()
	_ = d.updateState(&isLocked, nil)
	log.Printf("locked: %t\n", isLocked)
	// TODO: handle latch controls!!!
}

func (d *door) onSenseUnlocked() {
	isUnlocked := !d.senseUnlocked.Get()
	isLocked := !isUnlocked
	_ = d.updateState(&isLocked, nil)
	log.Printf("unlocked: %t\n", isUnlocked)
	// TODO: handle latch controls!!!
}

func (d *door) onSenseDoor() {
	isClosed := d.senseDoor.Get()
	_ = d.updateState(nil, &isClosed)
}

func (d *door) watch() {
	for {
		select {
		case state := <-d.events:
			d.notify(&state)
		case <-d.closing:
			return
		}
	}
}

func (d *door) notify(state *DoorState) {
	d.subsMutex.Lock()
	defer d.subsMutex.Unlock()
	s := d.subs
	for s != nil {
		// XXX: If any of those channels doesn't have capacity,
		//		this will block with critical consequences! Careful!
		s.c <- *state
		s = s.next
	}
}

func (d *door) Lock() error {
	panic("Not implemented")
}

func (d *door) Unlock() error {
	panic("Not implemented")
}

func (d *door) State() DoorState {
	d.stateMutex.RLock()
	defer d.stateMutex.RUnlock()
	return d.state
}

type sub struct {
	c    chan DoorState
	next *sub // linked list in door
}

func (d *door) Subscribe(c chan DoorState) <-chan DoorState {
	if c == nil {
		c = make(chan DoorState, 3)
	}

	newSub := &sub{c: c}

	d.subsMutex.Lock()
	defer d.subsMutex.Unlock()
	newSub.next = d.subs
	d.subs = newSub

	return c
}

func (d *door) Unsubscribe(c <-chan DoorState) error {
	var s, prev *sub
	s = d.subs
	for s != nil {
		if s.c != c {
			prev, s = s, s.next
			continue
		}
		d.subsMutex.Lock()
		defer d.subsMutex.Unlock()
		// remove s from the linked list
		if prev == nil {
			// this is the first item
			d.subs = s.next
		}
		prev.next = s.next
		return nil
	}
	return ErrNotSubscribed
}

func (d *door) Close() error {
	// TODO: Once!
	d.stateMutex.Lock()
	d.subsMutex.Lock()
	defer d.stateMutex.Unlock()
	defer d.subsMutex.Unlock()
	close(d.events)
	close(d.closing)
	s := d.subs
	for s != nil {
		close(s.c)
		s = s.next
	}

	// TODO: close pins
	return nil
}

func (d *door) createPins(pins PinsConfig) error {
	log.Print("Initializing GPIOs")
	type numPin struct {
		num  int
		pin  *gpio.Pin
		mode gpio.Mode
	}
	pl := [...]*numPin{
		{pins.LatchEnable, &(d.latchEnable), gpio.ModeOutput},
		{pins.LatchLock, &(d.latchLock), gpio.ModeOutput},
		{pins.LatchUnlock, &(d.latchUnlock), gpio.ModeOutput},
		{pins.SenseLocked, &(d.senseLocked), gpio.ModeInput},
		{pins.SenseUnlocked, &(d.senseUnlocked), gpio.ModeInput},
		{pins.SenseDoor, &(d.senseDoor), gpio.ModeInput},
	}
	cleanup := func() {
		for _, p := range pl {
			if *p.pin == nil {
				break
			}
			_ = (*p.pin).Close()
		}
	}
	for _, p := range pl {
		log.Printf("Opening %d in mode '%v'", p.num, p.mode)
		pin, err := gpio.OpenPin(p.num, p.mode)
		if err != nil {
			cleanup()
			return err
		}
		*p.pin = pin
	}
	return nil
}
