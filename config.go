package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

type Config struct {
	Door DoorConfig
	Web  WebConfig
}

var defaultCfg = Config{
	Door: DoorConfig{
		Pins: PinsConfig{
			LatchEnable: 17,
			LatchLock:   54,
			LatchUnlock: 57,

			SenseLocked:   16,
			SenseUnlocked: 7,
			SenseDoor:     6,
		},
		MaxMotorTime: 300,
	},
	Web: WebConfig{
		Listen: ":80",
	},
}

func checkConfig(c *Config) error {

	// check for non-negative pin numbers
	allPins := []int{
		c.Door.Pins.LatchEnable,
		c.Door.Pins.LatchLock,
		c.Door.Pins.LatchUnlock,
		c.Door.Pins.SenseLocked,
		c.Door.Pins.SenseUnlocked,
		c.Door.Pins.SenseDoor,
	}
	for _, v := range allPins {
		// TODO: check that pin numbers are unique
		if v <= 0 {
			return fmt.Errorf("%d: invalid pin number", v)
		}
	}

	// TODO: check TLS configuration, maybe also Web.Listen

	return nil
}

func LoadConfig(data []byte) (c *Config, err error) {
	var cfg Config
	cfg = defaultCfg // copy the default into the new structure
	if data != nil {
		err = json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
	}
	if err = checkConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func LoadConfigFromFile(filename string) (c *Config, err error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return LoadConfig(data)
}
