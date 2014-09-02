package main

import (
	"github.com/kzyapkov/gpio"
)

type Door interface {
	Watch()		Watcher		// Watcher notifies for sensor events
	Lock()		error		// Engage the latch, blocking
	Unlock()	error		// Disengage the latch, blocking
	State()		DoorState	// Get the current state
	Close()		error		// Clean up resources
}

type DoorState struct {
	locked			bool
	closed			bool
}

// Locked returns whether the latch is currently engaged or not.
// It will return the previous state if the latch is currently being
// manipulated.
func (ds *DoorState) Locked() bool {
	return ds.locked
}
// Closed indicates whether the door is closed or open.
func (ds *DoorState) Closed() bool {
	return ds.closed
}

type door struct {
	latchEnable		gpio.Pin
	latchEngage		gpio.Pin
	latchDisengage	gpio.Pin

	senseDoor		gpio.Pin
	senseLatch		gpio.Pin
}

func (d *door) Lock() error {
	panic("Not implemented")
}

func (d *door) Unlock() error {
	panic("Not implemented")
}

func (d *door) State() DoorState {
	panic("Not implemented")
}

func (d *door) Watch() Watcher {
	return &watcher{}
}

func (d *door) Close() error {
	return nil
}

type Watcher interface {
	Watch() <-chan DoorState
	Close() error
}

type watcher struct {
	door *door
}

func (w *watcher) Watch() <-chan DoorState {
	return nil
}

func (w *watcher) Close() error {
	return nil
}

func createPins(enablePin, engagePin, disengagePin, doorSense, latchSense int, d *door) error {
	type numPin struct {
		num int
		pin gpio.Pin
		mode gpio.Mode
	}
	pins := [...]*numPin{
		{enablePin,		d.latchEnable,		gpio.ModeOutput},
		{engagePin,		d.latchEngage,		gpio.ModeOutput},
		{disengagePin,	d.latchDisengage,	gpio.ModeOutput},
		{doorSense,		d.senseDoor,		gpio.ModeInput},
		{latchSense,	d.senseLatch,		gpio.ModeInput},
	}
	cleanup := func() {
		for _, p := range pins {
			if p.pin == nil {
				break
			}
			_ = p.pin.Close()
		}
	}
	for _, p := range pins {
		pin, err := gpio.OpenPin(p.num, p.mode)
		if err != nil {
			cleanup()
			return err
		}
		p.pin = pin
	}
	return nil
}

func NewDoor(enablePin, engagePin, disengagePin, doorSense, latchSense int) (Door, error){
	var err error
	d := &door{}
	err = createPins(enablePin, engagePin, disengagePin, doorSense, latchSense, d)
	if err != nil {
		return nil, err
	}

	// TODO: goroutine to watch the pins
	// TODO: Door.Close() ?
	return d, nil
}

