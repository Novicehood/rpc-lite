package server

import (
	"reflect"
	"sync"
)

var reflectTypePools = &typePools{
	pools: make(map[reflect.Type]*sync.Pool),
	New: func(t reflect.Type) interface{} {
		var argv reflect.Value

		if t.Kind() == reflect.Ptr { // reply must be ptr
			argv = reflect.New(t.Elem())
		} else {
			argv = reflect.New(t)
		}

		return argv.Interface()
	},
}

type typePools struct {
	mu    sync.RWMutex
	pools map[reflect.Type]*sync.Pool
	New   func(t reflect.Type) interface{}
}

func (tp *typePools) Init(t reflect.Type) {
	p := &sync.Pool{}
	p.New = func() interface{} {
		return tp.New(t)
	}
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.pools[t] = p
}
