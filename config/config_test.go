package config_test

import (
	"testing"

	"github.com/kzyapkov/pesho/config"
)

func TestDefaultConfig(t *testing.T) {
	c, err := config.LoadFromBytes([]byte("{}"))
	if err != nil {
		t.Error(err)
	}
	if c.Web.TLS != nil {
		t.Error("TLS should not be configured by default")
	}
	t.Logf("%#v", c)
}

func TestLoadSomeConfigValues(t *testing.T) {
	c, err := config.LoadFromBytes([]byte(`{"Door":{"Pins":{"SenseUnlocked":99}}}`))
	if err != nil {
		t.Error(err)
	}
	if c.Door.Pins.SenseUnlocked != 99 {
		t.Errorf("Door.Pins.SenseUnlocked was not updated")
	}
}
