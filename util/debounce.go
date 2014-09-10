package util

import (
	"time"

	"github.com/kzyapkov/gpio"
)

type DebouncedInput struct {
	gpio.Pin
	SettleDuration time.Duration
	lastTime       time.Time
	callback       gpio.IRQEvent
}

func Debounced(pin gpio.Pin, settleDuration time.Duration) *DebouncedInput {
	pin.SetMode(gpio.ModeInput)
	return &DebouncedInput{
		Pin:            pin,
		SettleDuration: settleDuration,
		lastTime:       time.Now(),
	}
}

func (dbp *DebouncedInput) see() {
	now := time.Now()
	if now.Sub(dbp.lastTime) > dbp.SettleDuration {
		dbp.lastTime = now
		dbp.callback()
	}
}

func (dbp *DebouncedInput) BeginWatch(edge gpio.Edge, f gpio.IRQEvent) error {
	dbp.callback = f
	return dbp.Pin.BeginWatch(edge, dbp.see)
}
