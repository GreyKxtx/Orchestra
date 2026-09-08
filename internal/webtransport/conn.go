// Package webtransport carries Orchestra's JSON-RPC protocol over a WebSocket.
//
// jsonrpc.NewServer is transport-agnostic — it wants an io.Reader and an
// io.Writer carrying LSP framing (protocol/jsonrpc/codec.go:14). A WebSocket is
// already message-framed, so the browser should never see a Content-Length
// header. Pipe is the translation, and it is the whole of it: everything above
// (agent/event streaming, permission/request, question/ask, cancellation) is
// the existing server, unmodified.
package webtransport

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/coder/websocket"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

// Pipe adapts one *websocket.Conn to the Reader/Writer pair jsonrpc.NewServer
// wants. Each WebSocket frame carries exactly one JSON-RPC message.
type Pipe struct {
	conn *websocket.Conn

	// inbound: frames -> framed bytes for jsonrpc.Reader.
	inR *io.PipeReader
	inW *io.PipeWriter

	// outbound: framed bytes from jsonrpc.Writer -> frames.
	outR *io.PipeReader
	outW *io.PipeWriter

	// done is closed the moment the socket stops delivering — a dropped tab, a
	// normal close, a network error. Callers cancel the connection's context on
	// it; see Done for why that cannot wait for Serve to return.
	done     chan struct{}
	doneOnce sync.Once

	closeOnce sync.Once
	closeErr  error
}

// NewPipe starts the two pumps. It does not take ownership of ctx's lifetime
// beyond using it for socket reads and writes; call Close to release them.
func NewPipe(ctx context.Context, c *websocket.Conn) *Pipe {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p := &Pipe{conn: c, inR: inR, inW: inW, outR: outR, outW: outW, done: make(chan struct{})}
	go p.pumpInbound(ctx)
	go p.pumpOutbound(ctx)
	return p
}

// Done is closed when the socket stops delivering messages.
//
// EOF alone is not enough to end a connection. jsonrpc.Server.Serve waits for
// its in-flight Handle goroutines before returning (protocol/jsonrpc/server.go:96),
// and a handler blocked in Server.Request is waiting on a context derived from
// the very context the caller would cancel after Serve returns — so cancelling
// there deadlocks. Watch this channel instead and cancel as soon as the socket
// dies, which is what makes a pending permission/request fail closed.
func (p *Pipe) Done() <-chan struct{} { return p.done }

func (p *Pipe) markDone() { p.doneOnce.Do(func() { close(p.done) }) }

// Reader yields LSP-framed bytes, one frame per message, for jsonrpc.Reader.
func (p *Pipe) Reader() io.Reader { return p.inR }

// Writer accepts LSP-framed bytes from jsonrpc.Writer.
func (p *Pipe) Writer() io.Writer { return p.outW }

// Close is idempotent. It unblocks Reader with io.EOF, which is what makes
// jsonrpc.Server.Serve return on a dropped connection.
func (p *Pipe) Close() error {
	p.closeOnce.Do(func() {
		p.markDone()
		_ = p.inW.Close()
		_ = p.outW.Close()
		_ = p.outR.Close()
		p.closeErr = p.conn.Close(websocket.StatusNormalClosure, "")
	})
	return p.closeErr
}

// pumpInbound turns each received frame into one framed message. A read error
// (including a normal close) closes the inbound pipe, which reaches the JSON-RPC
// server as io.EOF.
func (p *Pipe) pumpInbound(ctx context.Context) {
	for {
		_, data, err := p.conn.Read(ctx)
		if err != nil {
			p.markDone()      // -> the connection's ctx is cancelled
			_ = p.inW.Close() // -> io.EOF for the reader
			return
		}
		header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
		if _, err := io.WriteString(p.inW, header); err != nil {
			return
		}
		if _, err := p.inW.Write(data); err != nil {
			return
		}
	}
}

// pumpOutbound parses the framing the JSON-RPC writer produces and sends each
// message as exactly one text frame. jsonrpc.Reader is reused here because it is
// already precisely a Content-Length parser, limits included.
func (p *Pipe) pumpOutbound(ctx context.Context) {
	r := jsonrpc.NewReader(p.outR)
	for {
		msg, err := r.ReadMessage()
		if err != nil {
			return
		}
		if err := p.conn.Write(ctx, websocket.MessageText, msg); err != nil {
			return
		}
	}
}
