package toml

import (
	"reflect"
)

// Toml is used for holding the decoded state of a toml config file.
type Toml map[string]any

// RecursiveModify will apply toml modifications at the current depth,
// then recurse for new depths.
func RecursiveModify(c map[string]any, modifications Toml) error {
	for key, value := range modifications {
		if reflect.ValueOf(value).Kind() == reflect.Map {
			cV, ok := c[key]
			if !ok {
				// Did not find section in existing config, populating fresh.
				cV = make(Toml)
			}
			// Retrieve existing config to apply overrides to.
			cVM, ok := cV.(map[string]any)
			if !ok {
				// if the config does not exist, we should create a blank one to allow creation
				cVM = make(Toml)
			}
			if err := RecursiveModify(cVM, value.(Toml)); err != nil {
				return err
			}
			c[key] = cVM
		} else {
			// Not a map, so we can set override value directly.
			c[key] = value
		}
	}
	return nil
}
