package driver

import (
	"fmt"
	"sort"
	"sync"
)

var (
	regMu    sync.RWMutex
	registry = map[string]Driver{}
)

func Register(d Driver) {
	regMu.Lock()
	defer regMu.Unlock()
	name := d.Name()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("driver already registered: %s", name))
	}
	registry[name] = d
}

func Get(name string) (Driver, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	d, ok := registry[name]
	return d, ok
}

func Names() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
