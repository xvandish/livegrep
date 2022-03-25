package main

import (
	"flag"
	"fmt"
	"strings"
)

// stringMapFlag implements flag.Value and provides the ability to specify multiple, arbitrary
// key-value parameters, presented as a map of string -> interface{}.
type stringMapFlag struct {
	flag.Value

	values map[string]interface{}
}

func newStringMapFlag() *stringMapFlag {
	return &stringMapFlag{
		values: make(map[string]interface{}),
	}
}

func (sm *stringMapFlag) Set(value string) error {
	components := strings.Split(value, "=")
	if len(components) != 2 {
		return fmt.Errorf("improperly formatted key-value parameter")
	}

	sm.values[components[0]] = components[1]

	return nil
}

func (sm *stringMapFlag) Values() map[string]interface{} {
	return sm.values
}

func (sm *stringMapFlag) String() string {
	var kvPairs []string

	for key, value := range sm.values {
		kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(kvPairs, ",")
}
