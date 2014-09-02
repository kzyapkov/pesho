package main

import (
	"testing"
	"encoding/json"
	"fmt"
)

func TestPrintSample(t *testing.T) {
	c := &Config{}
	fmt.Println(json.MarshalIndent(c, "", "  "))
}
