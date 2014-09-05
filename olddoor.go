// +build skip

package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kzyapkov/gpio"
)

type Door interface {
	Subscribe(chan DoorState) <-chan DoorState // Returns a channel of sensor events
	Unsubscribe(<-chan DoorState) error        // Channel will get no more notifications of events
	Lock() error                               // Engage the latch, blocking
	Unlock() error                             // Disengage the latch, blocking
	State() DoorState                          // Get the current state
	Close() error                              // Clean up resources
}

var (
	ErrAlreadyThere  = errors.New("already in the requested state")
	ErrInFlight      = errors.New("currently locking or unlocking")
	ErrNotSubscribed = errors.New("not currently subscribed")
	ErrDoorOpen      = errors.New("door is open")
	ErrTimeout       = errors.New("timeout while moving the latch")
)

type DoorState struct {
	Locked   bool      `json:"locked"`    // The state of the mechanical latch, true=locked
	Closed   bool      `json:"closed"`    // Whether the door is closed or ajar
	InFlight bool      `json:"in_flight"` // Whether we are currently changing Locked, in which case Locked is the old value
	When     time.Time `json:"when"`      // When did this event occur
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

	maxMotorTime time.Duration

	// conditions to signal changes in state
	stateLock sync.RWMutex
	state     DoorState
	events    chan DoorState

	subsMutex sync.Mutex
	subs      *sub
	closing   chan bool
}

// NewDoorFromConfig returns a door for the given configuration
func NewDoorFromConfig(cfg DoorConfig) (Door, error) {
	var err error
	d := &door{
		events:       make(chan DoorState, 15),
		maxMotorTime: time.Duration(cfg.MaxMotorTime) * time.Millisecond,
	}

	if err = d.createPins(cfg.Pins); err != nil {
		return nil, err
	}

	// find out the initial state
	if err = d.initState(); err != nil {
		d.Close()
		return nil, err
	}

	// install GPIO input callbacks
	d.senseLocked.BeginWatch(gpio.EdgeBoth, d.onSenseLocked)
	d.senseUnlocked.BeginWatch(gpio.EdgeBoth, d.onSenseUnlocked)
	d.senseDoor.BeginWatch(gpio.EdgeBoth, d.onSenseDoor)

	// this notifies subscribers for events
	go d.watch()

	return d, nil
}

func (d *door) initState() error {
	d.stateLock.Lock()
	defer d.stateLock.Unlock()

	d.state.Closed = d.senseDoor.Get()

	isLocked := d.senseLocked.Get()
	isUnlocked := d.senseUnlocked.Get()
	if isLocked == isUnlocked {
		return fmt.Errorf("Invalid sensor state: locked %t unlocked %t", isLocked, isUnlocked)
		// XXX: for safety, maybe we should try unlocking the door here and reach a valid state?
	}
	d.state.Locked = isLocked

	d.state.When = time.Now()
	return nil
}

func (d *door) onSenseLocked() {
	d.stateLock.Lock()
	defer d.stateLock.Unlock()

	isLocked := d.senseLocked.Get()

	if d.state.Locked == isLocked {
		// already locked, probably mechanical bounces in the switch
		log.Printf("ignoring locked event %t", isLocked)
		return
	}

	if d.state.InFlight && !d.state.Locked {
		// We're moving the latch to locked position, and it hit it.
		d.latchEnable.Clear()
		d.latchLock.Clear()
		d.latchUnlock.Clear()
		d.state.InFlight = false
		d.state.Locked = true
	} else {
		fmt.Printf("locked event '%t' with no effect, state %v", isLocked, d.state)
	}
	d.events <- d.state
}

func (d *door) onSenseUnlocked() {
	d.stateLock.Lock()
	defer d.stateLock.Unlock()

	isUnlocked := d.senseUnlocked.Get()

	if !d.state.Locked == isUnlocked {
		log.Printf("ignoring unlocked event %t", isUnlocked)
		return
	}

	if d.state.InFlight && d.state.Locked {
		// We're unlocking the door, and we hit the unlock position sensor.
		d.latchEnable.Clear()
		d.latchLock.Clear()
		d.latchUnlock.Clear()
		d.state.InFlight = false
		d.state.Locked = false
	} else {
		fmt.Printf("unlocked event with no effect %t", isUnlocked)
	}
	d.events <- d.state
}

func (d *door) onSenseDoor() {
	d.stateLock.Lock()
	defer d.stateLock.Unlock()

	if isClosed := d.senseDoor.Get(); isClosed != d.state.Closed {
		d.state.Closed = isClosed
		d.events <- d.state
	}
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
		if cap(s.c) > 0 {
			s.c <- *state
		} else {
			log.Printf("skip notifying %v, no capacity", s)
		}
		s = s.next
	}
}

func (d *door) Lock() error {
	d.stateLock.Lock()
	d.stateLock.Unlock()

	if d.state.Locked {
		d.stateLock.Unlock()
		return ErrAlreadyThere
	}
	if !d.state.Closed {
		d.stateLock.Unlock()
		return ErrDoorOpen
	}
	if d.state.InFlight {
		// Lock requested while locking
		d.stateLock.Unlock()
		return d.waitForLatch()
	}

	d.state.InFlight = true
	d.latchLock.Set()   // direction for locking
	d.latchEnable.Set() // let the current flow

	d.stateLock.Unlock() // let the callbacks update the state

	return d.waitForLatch()
}

func (d *door) Unlock() error {
	d.stateLock.Lock()
	defer d.stateLock.Unlock()

	if !d.state.Locked {
		return ErrAlreadyThere
	}
	if d.state.InFlight {
		return d.waitForLatch()
	}

	d.state.InFlight = true
	d.latchUnlock.Set()
	d.latchEnable.Set()

	return d.waitForLatch()
}

func (d *door) waitForLatch() error {
	updates := d.Subscribe(nil)
	defer d.Unsubscribe(updates)

	done := make(chan error)
	timer := time.AfterFunc(d.maxMotorTime, func() {
		d.stateLock.Lock()
		if !d.state.InFlight {
			d.stateLock.Unlock()
			return
		}
		d.state.InFlight = false
		d.stateLock.Unlock()

		d.latchEnable.Clear()
		d.latchLock.Clear()
		d.latchUnlock.Clear()
		done <- ErrTimeout
	})

	timer = time.NewTimer(d.maxMotorTime)

	for {
		select {
		case <-timer.C:
			d.stateLock.Lock()
			if !d.state.InFlight {
				d.stateLock.Unlock()
				return nil
			}

			d.state.InFlight = false
			d.stateLock.Unlock()

			d.latchEnable.Clear()
			d.latchLock.Clear()
			d.latchUnlock.Clear()

			return ErrTimeout

		case state := <-updates:
			if state.InFlight {
				continue
			}
			timer.Stop()
			return nil
		}
	}
}

func (d *door) State() DoorState {
	d.stateLock.RLock()
	defer d.stateLock.RUnlock()
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
	// TODO: Once!
	d.stateLock.Lock()
	d.subsMutex.Lock()
	defer d.stateLock.Unlock()
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
