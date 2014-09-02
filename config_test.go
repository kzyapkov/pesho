package main

import (
	"encoding/json"
	"testing"
)

func TestPrintDefault(t *testing.T) {
	c, err := LoadConfig(nil)
	if err != nil {
		t.Error(err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Error(err)
	}
	t.Log(string(data))
}

func TestDefaultConfig(t *testing.T) {
	c, err := LoadConfig([]byte("{}"))
	if err != nil {
		t.Error(err)
	}
	if c.Web.TLS != nil {
		t.Error("TLS should not be configured by default")
	}
	t.Logf("%#v", c)
}

func TestLoadSomeConfigValues(t *testing.T) {
	c, err := LoadConfig([]byte(`{"Door":{"Pins":{"SenseUnlocked":99}}}`))
	if err != nil {
		t.Error(err)
	}
	if c.Door.Pins.SenseUnlocked != 99 {
		t.Errorf("Door.Pins.SenseUnlocked was not updated")
	}
}
