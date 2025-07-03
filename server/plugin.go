package server

import (
	"context"
	"github.com/Novicehood/rpc-lite/errors"
	"github.com/Novicehood/rpc-lite/protocol"
	"github.com/julienschmidt/httprouter"
	"github.com/soheilhy/cmux"
	"net"
	"net/http"
)

type Plugin interface{}

type (
	// RegisterPlugin is .
	RegisterPlugin interface {
		Register(name string, rcvr interface{}, metadata string) error
		Unregister(name string) error
	}

	// RegisterFunctionPlugin is .
	RegisterFunctionPlugin interface {
		RegisterFunction(serviceName, fname string, fn interface{}, metadata string) error
	}

	// PostConnAcceptPlugin represents connection accept plugin.
	// if returns false, it means subsequent IPostConnAcceptPlugins should not continue to handle this conn
	// and this conn has been closed.
	PostConnAcceptPlugin interface {
		HandleConnAccept(net.Conn) (net.Conn, bool)
	}

	// PostConnClosePlugin represents client connection close plugin.
	PostConnClosePlugin interface {
		HandleConnClose(net.Conn) bool
	}

	// PreReadRequestPlugin represents .
	PreReadRequestPlugin interface {
		PreReadRequest(ctx context.Context) error
	}

	// PostReadRequestPlugin represents .
	PostReadRequestPlugin interface {
		PostReadRequest(ctx context.Context, r *protocol.Message, e error) error
	}

	// PostHTTPRequestPlugin represents .
	PostHTTPRequestPlugin interface {
		PostHTTPRequest(ctx context.Context, r *http.Request, params httprouter.Params) error
	}

	// PreHandleRequestPlugin represents .
	PreHandleRequestPlugin interface {
		PreHandleRequest(ctx context.Context, r *protocol.Message) error
	}

	PreCallPlugin interface {
		PreCall(ctx context.Context, serviceName, methodName string, args interface{}) (interface{}, error)
	}

	PostCallPlugin interface {
		PostCall(ctx context.Context, serviceName, methodName string, args, reply interface{}, err error) (interface{}, error)
	}

	// PreWriteResponsePlugin represents .
	PreWriteResponsePlugin interface {
		PreWriteResponse(context.Context, *protocol.Message, *protocol.Message, error) error
	}

	// PostWriteResponsePlugin represents .
	PostWriteResponsePlugin interface {
		PostWriteResponse(context.Context, *protocol.Message, *protocol.Message, error) error
	}

	// PreWriteRequestPlugin represents .
	PreWriteRequestPlugin interface {
		PreWriteRequest(ctx context.Context) error
	}

	// PostWriteRequestPlugin represents .
	PostWriteRequestPlugin interface {
		PostWriteRequest(ctx context.Context, r *protocol.Message, e error) error
	}

	// HeartbeatPlugin is .
	HeartbeatPlugin interface {
		HeartbeatRequest(ctx context.Context, req *protocol.Message) error
	}

	CMuxPlugin interface {
		MuxMatch(m cmux.CMux)
	}
)

type PluginContainer interface {
	//Add(plugin Plugin)
	//Remove(plugin Plugin)
	//All() []Plugin

	DoRegister(name string, rcvr interface{}, metadata string) error
	//DoRegisterFunction(serviceName, fname string, fn interface{}, metadata string) errors
	DoUnregister(name string) error
	//
	//DoPostConnAccept(net.Conn) (net.Conn, bool)
	//DoPostConnClose(net.Conn) bool
	//
	//DoPreReadRequest(ctx context.Context) errors
	//DoPostReadRequest(ctx context.Context, r *protocol.Message, e errors) errors
	//DoPostHTTPRequest(ctx context.Context, r *http.Request, params httprouter.Params) errors
	//
	//DoPreHandleRequest(ctx context.Context, req *protocol.Message) errors
	//DoPreCall(ctx context.Context, serviceName, methodName string, args interface{}) (interface{}, errors)
	//DoPostCall(ctx context.Context, serviceName, methodName string, args, reply interface{}, err errors) (interface{}, errors)
	//
	//DoPreWriteResponse(context.Context, *protocol.Message, *protocol.Message, errors) errors
	//DoPostWriteResponse(context.Context, *protocol.Message, *protocol.Message, errors) errors
	//
	//DoPreWriteRequest(ctx context.Context) errors
	//DoPostWriteRequest(ctx context.Context, r *protocol.Message, e errors) errors
	//
	//DoHeartbeatRequest(ctx context.Context, req *protocol.Message) errors
	//
	//MuxMatch(m cmux.CMux)
}

type pluginContainer struct {
	plugins []Plugin
}

func (p *pluginContainer) DoRegister(name string, rcvr interface{}, metadata string) error {
	var es []error
	for _, rp := range p.plugins {
		if plugin, ok := rp.(RegisterPlugin); ok {
			err := plugin.Register(name, rcvr, metadata)
			if err != nil {
				es = append(es, err)
			}
		}
	}
	if len(es) > 0 {
		return errors.NewMultiError(es)
	}
	return nil
}

func (p *pluginContainer) DoUnregister(name string) error {
	var es []error
	for _, rp := range p.plugins {
		if plugin, ok := rp.(RegisterPlugin); ok {
			err := plugin.Unregister(name)
			if err != nil {
				es = append(es, err)
			}
		}
	}
	if len(es) > 0 {
		return errors.NewMultiError(es)
	}
	return nil
}
