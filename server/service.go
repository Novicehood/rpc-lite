package server

import (
	"context"
	"errors"
	errors2 "github.com/Novicehood/rpc-lite/errors"
	"github.com/Novicehood/rpc-lite/log"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"
)

var typeOfContext = reflect.TypeOf((*context.Context)(nil)).Elem()

var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

type methodType struct {
	sync.Mutex
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
}

type functionType struct {
	sync.Mutex
	fn        reflect.Value
	ArgType   reflect.Type
	ReplyType reflect.Type
}

type service struct {
	name      string
	rcvr      reflect.Value
	typ       reflect.Type
	methods   map[string]*methodType
	functions map[string]*functionType
}

func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return isExported(t.Name()) || t.PkgPath() == ""
}

// suitableMethods returns suitable Rpc methods of typ, it will report
// errors using log if reportErr is true.
func suitableMethods(typ reflect.Type, reportErr bool) map[string]*methodType {
	methods := make(map[string]*methodType)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		mtype := method.Type
		mname := method.Name

		if mtype.NumIn() != 4 {
			if reportErr {
				log.Debug("method ", mname, " has wrong number of ins:", mtype.NumIn())
			}
			continue
		}

		// Method needs four ins: receiver, context.Context, *args, *reply.
		ctxType := mtype.In(1)
		if !ctxType.Implements(typeOfContext) {
			if reportErr {
				log.Debug("method ", mname, " must use context.Context as the first parameter")
			}
			continue
		}

		argType := mtype.In(2)
		if !isExportedOrBuiltinType(argType) {
			if reportErr {
				log.Info(mname, " parameter type not exported: ", argType)
			}
			continue
		}

		replyType := mtype.In(3)
		if replyType.Kind() != reflect.Ptr {
			if reportErr {
				log.Info("method", mname, " reply type not a pointer:", replyType)
			}
			continue
		}

		// Reply type must be exported
		if !isExportedOrBuiltinType(replyType) {
			if reportErr {
				log.Info("method", mname, " reply type not exported: ", replyType)
			}
			continue
		}

		// Method needs one out.
		if mtype.NumOut() != 1 {
			if reportErr {
				log.Info("method", mname, " has wrong number of outs:", mtype.NumOut())
			}
			continue
		}

		// The return type of the method must be errors.
		if returnType := mtype.Out(0); returnType != typeOfError {
			if reportErr {
				log.Info("method", mname, " returns ", returnType.String(), " not errors")
			}
			continue
		}
		methods[mname] = &methodType{method: method, ArgType: argType, ReplyType: replyType}
		// init pool for reflect.Type of args and reply
		reflectTypePools.Init(argType)
		reflectTypePools.Init(replyType)
	}
	return methods
}

func (s *Server) register(rcvr interface{}, name string, usename bool) (string, error) {
	s.serviceMapMu.Lock()
	defer s.serviceMapMu.Unlock()
	service := new(service)
	service.typ = reflect.TypeOf(rcvr)
	service.rcvr = reflect.ValueOf(rcvr)
	srvname := reflect.Indirect(service.rcvr).Type().Name()
	if usename {
		srvname = name
	}
	if len(srvname) == 0 {
		errorStr := "rpcx.Register: no service name for type " + service.typ.String()
		log.Error(errorStr)
		return srvname, errors.New(errorStr)
	}
	if !usename && !isExported(srvname) {
		errorStr := "rpcx.Register: type " + srvname + " is not exported"
		log.Error(errorStr)
		return srvname, errors.New(errorStr)
	}
	service.name = srvname
	service.methods = suitableMethods(service.typ, true)

	if len(service.methods) == 0 {
		var errorStr string

		methods := suitableMethods(reflect.PtrTo(service.typ), false)
		if len(methods) != 0 {
			errorStr = "rpcx.Register: type " + srvname + " has no exported methods of suitable type (hint: pass a pointer to value of that type)"
		} else {
			errorStr = "rpcx.Register: type " + srvname + " has no exported methods of suitable type"
		}
		log.Error(errorStr)
		return srvname, errors.New(errorStr)
	}

	s.serviceMap[service.name] = service
	return srvname, nil
}

func (s *Server) RegisterName(name string, rcvr interface{}, metadata string) error {
	_, err := s.register(rcvr, name, true)
	if err != nil {
		return err
	}
	if s.Plugins == nil {
		s.Plugins = &pluginContainer{}
	}
	return s.Plugins.DoRegister(name, rcvr, metadata)
}

func (s *Server) UnregisterAll() error {
	var err error
	s.unregisterAllOnce.Do(func() {
		err = s.unregisterAll()
	})
	return err
}

func (s *Server) unregisterAll() (err error) {
	s.serviceMapMu.RLock()
	defer s.serviceMapMu.RUnlock()
	var es []error
	for k := range s.serviceMap {
		err := s.Plugins.DoUnregister(k)
		if err != nil {
			es = append(es, err)
		}
	}

	if len(es) > 0 {
		return errors2.NewMultiError(es)
	}
	return nil
}
