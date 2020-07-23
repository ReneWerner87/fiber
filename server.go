package fiber

import (
	"bufio"
	"bytes"
	"fmt"
	gnet "github.com/panjf2000/gnet"
	fasthttp "github.com/valyala/fasthttp"
	"io"
	"log"
	"sync"
)

type request struct {
	proto, method string
	path, query   string
	head, body    string
	remoteAddr    string
}
type httpServer struct {
	*gnet.EventServer
}

type httpCodec struct {
	req request
}

func (hc *Server) Encode(c gnet.Conn, buf []byte) (out []byte, err error) {
	if c.Context() == nil {
		return buf, nil
	}
	// TODO: add output response -> error
	return buf, nil
}

func (s *Server) Decode(c gnet.Conn) (out []byte, err error) {
	buf := c.Read()
	c.ResetBuffer()

	if len(buf) == 0 {
		return
	}
	ctx := s.acquireCtx(c)
	ctx.reader = bytes.NewReader(buf)
	ctx.writer = bytes.NewBuffer(out)
	var (
		br *bufio.Reader
		bw *bufio.Writer
	)
	if br == nil {
		br = acquireReader(ctx)
	}

	// reading Headers
	if err = ctx.Request.Header.Read(br); err == nil {
		//read body
		err = ctx.Request.ContinueReadBody(br, fasthttp.DefaultMaxRequestBodySize, s.GetOnly, !s.DisablePreParseMultipartForm)
	}
	releaseReader(s, br)
	if bw == nil {
		bw = acquireWriter(ctx)
	}

	if err != nil {
		bw = s.writeErrorResponse(bw, ctx, getBytes("serverName"), err)
	}
	br = nil
	s.Handler(ctx)

	if err = writeResponse(ctx, bw); err != nil {
		log.Fatal(err)
	}

	bw.Flush()
	out = ctx.writer.Bytes()
	if br != nil {
		releaseReader(s, br)
	}
	if bw != nil {
		releaseWriter(s, bw)
	}

	if ctx != nil {
		ctx.Request.Reset()
		s.releaseCtx(ctx)
	}

	return out, nil
}

func serve(app *App, addr string) error {

	http := new(httpServer)
	hc := new(Server)
	hc.Handler = app.Handler()

	fmt.Println(addr)
	// Start serving!
	return gnet.Serve(http, fmt.Sprintf("tcp://%s", addr), gnet.WithMulticore(true), gnet.WithCodec(hc))
}

type RequestHandler func(ctx *RequestCtx)
type Server struct {
	fasthttp.Server
	ctxPool      sync.Pool
	readerPool   sync.Pool
	writerPool   sync.Pool
	Handler      RequestHandler
	ErrorHandler func(ctx *RequestCtx, err error)
}
type Request struct {
	fasthttp.Request
	keepBodyBuffer bool
	s              *Server
}
type Response struct {
	fasthttp.Response
	keepBodyBuffer bool
}
type RequestCtx struct {
	fasthttp.RequestCtx
	Request  Request
	Response Response
	c        gnet.Conn
	s        *Server

	reader io.Reader
	writer *bytes.Buffer
}

func (s *Server) acquireCtx(c gnet.Conn) (ctx *RequestCtx) {
	v := s.ctxPool.Get()
	if v == nil {
		ctx = &RequestCtx{
			s: s,
		}
		keepBodyBuffer := !s.ReduceMemoryUsage
		ctx.Request.keepBodyBuffer = keepBodyBuffer
		ctx.Response.keepBodyBuffer = keepBodyBuffer
	} else {
		ctx = v.(*RequestCtx)
	}
	ctx.c = c

	return
}

func acquireReader(ctx *RequestCtx) *bufio.Reader {
	v := ctx.s.readerPool.Get()
	if v == nil {
		n := ctx.s.ReadBufferSize
		if n <= 0 {
			n = defaultReadBufferSize
		}
		return bufio.NewReaderSize(ctx.reader, n)
	}
	r := v.(*bufio.Reader)
	r.Reset(ctx.reader)
	return r
}

func releaseReader(s *Server, r *bufio.Reader) {
	s.readerPool.Put(r)
}

func acquireWriter(ctx *RequestCtx) *bufio.Writer {
	v := ctx.s.writerPool.Get()
	if v == nil {
		n := ctx.s.WriteBufferSize
		if n <= 0 {
			n = defaultWriteBufferSize
		}
		return bufio.NewWriterSize(ctx.writer, n)
	}
	w := v.(*bufio.Writer)
	w.Reset(ctx.writer)
	return w
}

func releaseWriter(s *Server, w *bufio.Writer) {
	s.writerPool.Put(w)
}

func (s *Server) writeErrorResponse(bw *bufio.Writer, ctx *RequestCtx, serverName []byte, err error) *bufio.Writer {
	s.ErrorHandler(ctx, err)

	if serverName != nil {
		ctx.Response.Header.SetServerBytes(serverName)
	}
	ctx.SetConnectionClose()
	if bw == nil {
		bw = acquireWriter(ctx)
	}
	writeResponse(ctx, bw) //nolint:errcheck
	bw.Flush()
	return bw
}
func writeResponse(ctx *RequestCtx, w *bufio.Writer) error {
	//if ctx.timeoutResponse != nil {
	//	panic("BUG: cannot write timed out response")
	//}
	err := ctx.Response.Write(w)
	ctx.Response.Reset()
	return err
}

func (s *Server) releaseCtx(ctx *RequestCtx) {
	//if ctx.timeoutResponse != nil {
	//	panic("BUG: cannot release timed out RequestCtx")
	//}
	ctx.c = nil
	s.ctxPool.Put(ctx)
}
