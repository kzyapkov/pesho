package main

import (
	"fmt"
	"encoding/json"
)

type User struct {
	Name		string
	Password	string
}

type Config struct {
	Pins struct {
		LatchEnable		int
		LatchLock		int
		LatchUnlock		int

		SenseLocked		int
		SenseUnlocked	int
		SenseDoor		int
	}

	Web struct {
		Listen			string
		Users			[]User
	}
}

func printSampleConfig() {
	c := &Config{}
	fmt.Println(json.MarshalIndent(c, "", "  "))
}
