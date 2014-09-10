package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kzyapkov/pesho/door"
)

func TestStateMarshalling(t *testing.T) {
	state_p := &door.State{
		Latch: door.Locking,
		Door:  door.Closed,
	}
	data, err := json.Marshal(state_p)
	if err != nil {
		t.Error(err)
	}
	data_s := string(data)
	if !strings.Contains(data_s, "locking") {
		t.Error("didn't find locking in marshalled output")
	}
}
