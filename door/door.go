// Package door implements the interface to an actual door lock and its
// associated sensors, controls and logic.
package door

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/kzyapkov/gpio"
	"github.com/kzyapkov/pesho/config"
)

var (
	ErrNotSubscribed = errors.New("not currently subscribed")
	ErrInterrupted   = errors.New("the requested door operation was interrupted")
	ErrBusy          = errors.New("Pesho is busy doing something important")
)

type Door struct {
	sensors  *sensors
	controls *controls

	state ProtectedState

	lastState State

	dying chan struct{}

	subsMutex *sync.Mutex
	subs      *sub
	events    chan DoorEvent

	closeOnce sync.Once
}

type ProtectedState struct {
	*State
	*sync.Mutex
}

func (ps *ProtectedState) lockedGet() (s State) {
	ps.Lock()
	s = *ps.State
	ps.Unlock()
	return
}

func (ps *ProtectedState) lockedSet(new State) (changed bool, old State) {
	ps.Lock()
	old = *ps.State
	*ps.State = new
	ps.Unlock()
	changed = !old.Same(&new)
	return
}

const (
	DoorHWEvent int = iota
	LockHWEvent
)

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
			// log.Printf("monitorPins: door=%s; locked=%s; unlocked=%s",
			// 	d.sensors.door.Get(), d.sensors.locked.Get(), d.sensors.unlocked.Get())

			s := d.state.lockedGet()
			if s.IsInFlight() {
				continue
			}
			var new State
			d.sensors.fetchDoorState(&new)
			d.sensors.fetchLatchState(&new)
			if changed, old := d.state.lockedSet(new); changed {
				log.Printf("monitorPins: notifying for %s -> %s",
					old.String(), new.String())
				d.notify(old, new)
			} else {
				log.Printf("monitorPins: pins moved, but no door state change")
			}
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

	old := *d.state.State
	log.Printf("moveLatch: now %s", inFlight.String())
	d.state.Latch = inFlight
	d.state.Unlock()

	var ret error
	<-time.After(200 * time.Millisecond)
	d.controls.stopMotor()
	<-time.After(150 * time.Millisecond)

	var new State
	d.state.Lock()
	d.sensors.fetchDoorState(&new)
	d.sensors.fetchLatchState(&new)

	d.state.Door = new.Door
	d.state.Latch = new.Latch

	if (new.IsLocked() && target == Locked) || (new.IsUnlocked() && target == Unlocked) {
		ret = nil
	} else {
		ret = ErrBusy
	}

	d.notify(old, new)

	log.Printf("moveLatch: state at exit: %s", new.String())
	return ret
}

func (d *Door) notify(old, new State) {
	// if old.Same(&new) {
	// 	return
	// }
	go func() {
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

func (d *Door) Close() {
	d.closeOnce.Do(d.close)
}

func (d *Door) close() {
	close(d.dying)
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
			locked:   mustPin(cfg.Pins.SenseLocked, gpio.ModeInput),
			unlocked: mustPin(cfg.Pins.SenseUnlocked, gpio.ModeInput),
			door:     mustPin(cfg.Pins.SenseDoor, gpio.ModeInput),
		},
		controls: &controls{
			enable: mustPin(cfg.Pins.LatchEnable, gpio.ModeLow),
			lock:   mustPin(cfg.Pins.LatchLock, gpio.ModeLow),
			unlock: mustPin(cfg.Pins.LatchUnlock, gpio.ModeLow),
		},
		subsMutex: &sync.Mutex{},
	}

	d.dying = make(chan struct{})
	d.state.State = &State{}
	d.state.Mutex = &sync.Mutex{}
	d.state.State.Door = NotChecked
	d.state.State.Latch = Unknown

	var found State
	d.sensors.fetchDoorState(&found)
	d.sensors.fetchLatchState(&found)
	_, old := d.state.lockedSet(found)
	d.notify(old, found)

	go d.monitorPins()

	return d, nil
}
