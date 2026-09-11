package mesher

import (
	"fmt"
	"sort"
)

var registry = map[string]func() Mesher{}

// Register makes an algorithm available by name. Call it from an init function
// in the algorithm's package; importing that package is what wires it in.
func Register(name string, factory func() Mesher) {
	if _, dup := registry[name]; dup {
		panic("mesher: duplicate registration for " + name)
	}
	registry[name] = factory
}

// New constructs a registered algorithm by name.
func New(name string) (Mesher, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("mesher: unknown algorithm %q (have: %v)", name, List())
	}
	return factory(), nil
}

// List returns the registered algorithm names, sorted.
func List() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
