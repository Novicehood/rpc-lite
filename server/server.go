package server

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/Novicehood/rpc-lite/log"
	"github.com/Novicehood/rpc-lite/protocol"
	"github.com/Novicehood/rpc-lite/share"
	"github.com/soheilhy/cmux"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrServerClosed  = errors.New("http: Server closed")
	ErrReqReachLimit = errors.New("request reached rate limit")
)

type Handler func(ctx context.Context) error

type WorkerPool interface {
	Submit(task func())
	StopAndWaitFor(deadline time.Duration)
	Stop() context.Context
	StopAndWait()
}

type Server struct {
	ln                net.Listener
	readTimeout       time.Duration
	writeTimeout      time.Duration
	gatewayHTTPServer *http.Server

	jsonrpcHTTPServerLock sync.Mutex
	jsonrpcHTTPServer     *http.Server
	DisableHTTPGateway    bool
	DisableJsonRpc        bool
	AsyncWrite            bool
	pool                  WorkerPool

	serviceMapMu sync.RWMutex
	serviceMap   map[string]*service

	router map[string]Handler

	mu         sync.RWMutex
	activeConn map[net.Conn]struct{}
	doneChan   chan struct{}
	seq        atomic.Uint64

	inShutdown int32
	onShutdown []func(s *Server)
	onRestart  []func(s *Server)

	tlsConfig *tls.Config
	// BlockCrypt for kcp.BlockCrypt
	options map[string]interface{}
	// CORS options
	//corsOptions *CORSOptions

	Plugins PluginContainer

	AuthFunc func(ctx context.Context, req *protocol.Message, token string) (interface{}, error)

	handlerMsgNum int32
	requestCount  atomic.Uint64

	// HandleServiceError is used to get all service errors. You can use it write logs or others.
	HandleServiceError func(error)

	// ServerErrorFunc is a customized errors handlers and you can use it to return customized errors strings to clients.
	// If not set, it use err.Error()
	ServerErrorFunc func(res *protocol.Message, err error) string

	// The server is started.
	Started           chan struct{}
	unregisterAllOnce sync.Once
}

func NewServer(options ...OptionFn) *Server {
	s := &Server{
		Plugins:    &pluginContainer{},
		options:    make(map[string]interface{}),
		activeConn: make(map[net.Conn]struct{}),
		doneChan:   make(chan struct{}),
		serviceMap: make(map[string]*service),
		router:     make(map[string]Handler),
		AsyncWrite: false,
		Started:    make(chan struct{}),
	}

	for _, op := range options {
		op(s)
	}

	if s.options["TCPKeepAlivePeriod"] == nil {
		s.options["TCPKeepAlivePeriod"] = 3 * time.Minute
	}

	return s
}

func (s *Server) Serve(network, addr string) (err error) {
	var ln net.Listener
	ln, err = s.makeListener(network, addr)
	if err != nil {
		return err
	}

	defer s.UnregisterAll()

	if network == "http" {
		s.serveByHTTP(ln, "")
		return nil
	}

	if network == "ws" || network == "wss" {
		s.serveByWs(ln, "")
		return nil
	}

	ln = startGateway(network, ln)

	return s.serveListener(ln)
}

func (s *Server) isShutDown() bool {
	return atomic.LoadInt32(&s.inShutdown) == 1
}

func (s *Server) closeConn(conn net.Conn) {
	s.mu.Lock()
	delete(s.activeConn, conn)
	s.mu.Unlock()
	conn.Close()

	//TODO s.Plugins.DoPostConnClose(conn)
}

// TODO
func (s *Server) serveConn(conn net.Conn) {
	if s.isShutDown() {
		s.closeConn(conn)
		return
	}

}

var connected = "200 Connected to rpcx"

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodConnect {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		io.WriteString(w, "405 must CONNECT\n")
		return
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Info("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	io.WriteString(conn, "HTTP/1.1 200 Connection established\n\n")

	s.mu.Lock()
	s.activeConn[conn] = struct{}{}
	s.mu.Unlock()
	s.serveConn(conn)
}

// serveByHTTP serves by HTTP.
// if rpcPath is an empty string, use share.DefaultRPCPath.
func (s *Server) serveByHTTP(ln net.Listener, rpcPath string) {
	s.ln = ln
	if rpcPath == "" {
		rpcPath = share.DefaultRPCPath
	}
	mux := http.NewServeMux()
	mux.Handle(rpcPath, s)

	srv := &http.Server{Handler: mux}
	srv.Serve(ln)
}

func (s *Server) serveListener(ln net.Listener) error {
	var tempDelay time.Duration
	s.mu.Lock()
	s.ln = ln
	close(s.Started)
	s.mu.Unlock()

	//监听是否有新的连接加入
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.isShutDown() {
				<-s.doneChan
				return ErrServerClosed
			}

			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}

				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}

				log.Errorf("rpcx: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}

			if errors.Is(err, cmux.ErrListenerClosed) {
				return ErrServerClosed
			}
			return err
		}

		tempDelay = 0

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			period := s.options["TCPKeepAlivePeriod"]
			if period != nil {
				tcpConn.SetKeepAlive(true)
				tcpConn.SetKeepAlivePeriod(period.(time.Duration))
				tcpConn.SetLinger(10)
			}
		}
		//conn, ok := s.Plugins.DoPostConnAccept(conn)
		s.mu.Lock()
		s.activeConn[conn] = struct{}{}
		s.mu.Unlock()

		if share.Trace {
			log.Debugf("server accepted an conn: %v", conn.RemoteAddr().String())
		}

		go s.serveConn(conn)

	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.ln != nil {
		err = s.ln.Close()
	}

	for conn := range s.activeConn {
		conn.Close()
		delete(s.activeConn, conn)
	}
	//s.Plugins.DoPostConnClose(c)

	s.closeDoneChanLocked()

	if s.pool != nil {
		s.pool.StopAndWaitFor(10 * time.Second)
	}

	return err
}

func (s *Server) closeDoneChanLocked() {
	select {
	case <-s.doneChan:
		// Already closed. Don't close again.
	default:
		// Safe to close here. We're the only closer, guarded
		// by s.mu.RegisterName
		close(s.doneChan)
	}
}
