// pesho, named after Peter the Saint is the gatekeeper
// of the premises of initlab.org
//
package main

import (
	"log"
)


// some text just above main
func main() {
	// TODO: load configuration


	d, err := NewDoor(1, 2, 3, 4, 5)
	if err != nil {
		log.Panicf("Could not init GPIOs: %v", err)
	}
	defer d.Close()


	// TODO: initialize the web service

	// TODO: run the door governor
}


