package main

import (
	"time"

	"github.com/kzyapkov/gpio"
	"github.com/kzyapkov/pesho/util"
)

type Button string

const RedButton Button = "RED"
const GreenButton Button = "GREEN"

type buttons struct {
	Presses chan Button
	red     gpio.Pin
	green   gpio.Pin
}

func newButtons(redN, greenN int) (*buttons, error) {
	var red, green gpio.Pin
	var err error
	if red, err = gpio.OpenPin(redN, gpio.ModeInput); err != nil {
		return nil, err
	}
	if green, err = gpio.OpenPin(greenN, gpio.ModeInput); err != nil {
		_ = red.Close()
		return nil, err
	}

	btns := &buttons{
		Presses: make(chan Button, 10),
		red:     util.Debounced(red, 20*time.Millisecond),
		green:   util.Debounced(green, 20*time.Millisecond),
	}

	btns.red.BeginWatch(gpio.EdgeFalling, func() {
		btns.Presses <- RedButton
	})

	btns.green.BeginWatch(gpio.EdgeFalling, func() {
		btns.Presses <- GreenButton
	})

	return btns, nil
}
