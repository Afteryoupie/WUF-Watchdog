// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

type connectionContextKey struct{}

// net/http cancels Request.Context on a TCP read EOF, including CloseWrite.
// Keep a separate connection lifetime: EOF alone still permits a response,
// while read resets, write failures, and full server closure cancel work.
// HTTP parsing and response streaming remain entirely in the standard library.
type clientConn struct {
	net.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *clientConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		// net/http deliberately interrupts its background read after a handler.
		if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			c.cancel()
		}
	}
	return n, err
}

func (c *clientConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err != nil {
		c.cancel()
	}
	return n, err
}

func (c *clientConn) Close() error { c.cancel(); return c.Conn.Close() }

type clientListener struct{ net.Listener }

func (l clientListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &clientConn{Conn: conn, ctx: ctx, cancel: cancel}, nil
}

func HTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1024 * 1024,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			if client, ok := conn.(*clientConn); ok {
				return context.WithValue(ctx, connectionContextKey{}, client.ctx)
			}
			return ctx
		}}
}

func Serve(server *http.Server, listener net.Listener) error {
	return server.Serve(clientListener{listener})
}

func responseContext(r *http.Request) (context.Context, func()) {
	if connection, ok := r.Context().Value(connectionContextKey{}).(context.Context); ok {
		ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
		stop := context.AfterFunc(connection, cancel)
		return ctx, func() { stop(); cancel() }
	}
	return r.Context(), func() {}
}
