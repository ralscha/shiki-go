// Package oniguruma hosts Shiki's unmodified Oniguruma WebAssembly engine.
package oniguruma

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed onig.wasm
var wasm []byte

var compilationCache = wazero.NewCompilationCache()

// Capture uses UTF-8 byte offsets. An unmatched capture has Start and End -1.
type Capture struct{ Start, End int }
type Match struct {
	Index    int
	Captures []Capture
}

// Engine owns an isolated WebAssembly instance. Its methods are safe for concurrent use.
type Engine struct {
	mu                                             sync.Mutex
	runtime                                        wazero.Runtime
	module                                         api.Module
	ctx                                            context.Context
	malloc, free, create, destroy, find, lastError api.Function
	nextID                                         uint32
	closed                                         bool
}

func New() (*Engine, error) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCompilationCache(compilationCache))
	fail := func(err error) (*Engine, error) { _ = r.Close(ctx); return nil, err }
	env := r.NewHostModuleBuilder("env")
	start := time.Now()
	env.NewFunctionBuilder().WithFunc(func() float64 { return float64(time.Since(start).Nanoseconds()) / 1e6 }).Export("emscripten_get_now")
	env.NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, dest, src, n uint32) {
		data, ok := m.Memory().Read(src, n)
		if !ok || !m.Memory().Write(dest, data) {
			panic("oniguruma: invalid memory copy")
		}
	}).Export("emscripten_memcpy_big")
	env.NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, size uint32) uint32 {
		mem := m.Memory()
		old := uint64(mem.Size())
		wanted := (uint64(size) + 65535) &^ 65535
		if wanted <= old {
			return 1
		}
		if wanted > 2147483648 {
			return 0
		}
		if _, ok := mem.Grow(uint32((wanted - old) / 65536)); ok {
			return 1
		}
		return 0
	}).Export("emscripten_resize_heap")
	if _, err := env.Instantiate(ctx); err != nil {
		return fail(err)
	}
	wasi := r.NewHostModuleBuilder("wasi_snapshot_preview1")
	wasi.NewFunctionBuilder().WithFunc(func(context.Context, api.Module, uint32, uint32, uint32, uint32) uint32 { return 0 }).Export("fd_write")
	if _, err := wasi.Instantiate(ctx); err != nil {
		return fail(err)
	}
	m, err := r.InstantiateWithConfig(ctx, wasm, wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		return fail(err)
	}
	e := &Engine{runtime: r, module: m, ctx: ctx}
	e.malloc = m.ExportedFunction("omalloc")
	e.free = m.ExportedFunction("ofree")
	e.create = m.ExportedFunction("createOnigScanner")
	e.destroy = m.ExportedFunction("freeOnigScanner")
	e.find = m.ExportedFunction("findNextOnigScannerMatch")
	e.lastError = m.ExportedFunction("getLastOnigError")
	if e.malloc == nil || e.find == nil {
		return fail(fmt.Errorf("oniguruma: incompatible WebAssembly exports"))
	}
	return e, nil
}

func (e *Engine) alloc(data []byte) (uint32, error) {
	result, err := e.malloc.Call(e.ctx, uint64(max(1, len(data))))
	if err != nil {
		return 0, err
	}
	p := uint32(result[0])
	if p == 0 {
		return 0, fmt.Errorf("oniguruma: allocation failed")
	}
	if !e.module.Memory().Write(p, data) {
		return 0, fmt.Errorf("oniguruma: invalid allocation")
	}
	return p, nil
}
func (e *Engine) release(p uint32) { _, _ = e.free.Call(e.ctx, uint64(p)) }

type Scanner struct {
	engine *Engine
	ptr    uint32
}

func (e *Engine) NewScanner(patterns []string) (*Scanner, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, fmt.Errorf("oniguruma: engine is closed")
	}
	ptrs, lens := make([]byte, len(patterns)*4), make([]byte, len(patterns)*4)
	for i, pattern := range patterns {
		p, err := e.alloc([]byte(pattern))
		if err != nil {
			return nil, err
		}
		defer e.release(p)
		binary.LittleEndian.PutUint32(ptrs[i*4:], p)
		binary.LittleEndian.PutUint32(lens[i*4:], uint32(len(pattern)))
	}
	pp, err := e.alloc(ptrs)
	if err != nil {
		return nil, err
	}
	defer e.release(pp)
	lp, err := e.alloc(lens)
	if err != nil {
		return nil, err
	}
	defer e.release(lp)
	result, err := e.create.Call(e.ctx, uint64(pp), uint64(lp), uint64(len(patterns)))
	if err != nil {
		return nil, err
	}
	if result[0] == 0 {
		msg := "invalid regular expression"
		if p, er := e.lastError.Call(e.ctx); er == nil && p[0] != 0 {
			b := make([]byte, 0)
			for i := uint32(p[0]); ; i++ {
				c, ok := e.module.Memory().ReadByte(i)
				if !ok || c == 0 {
					break
				}
				b = append(b, c)
			}
			msg = string(b)
		}
		return nil, fmt.Errorf("oniguruma: %s", msg)
	}
	return &Scanner{engine: e, ptr: uint32(result[0])}, nil
}

// Find returns the earliest match; ties use the first pattern. Options are
// Oniguruma's NotBeginString (1), NotEndString (2), NotBeginPosition (4).
func (s *Scanner) Find(text string, start, options int) (*Match, error) {
	e := s.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || s.ptr == 0 {
		return nil, fmt.Errorf("oniguruma: scanner is closed")
	}
	if start < 0 || start > len(text) {
		return nil, fmt.Errorf("oniguruma: invalid start offset %d", start)
	}
	p, err := e.alloc([]byte(text))
	if err != nil {
		return nil, err
	}
	defer e.release(p)
	e.nextID++
	if e.nextID == 0 {
		e.nextID++
	}
	result, err := e.find.Call(e.ctx, uint64(s.ptr), uint64(e.nextID), uint64(p), uint64(len(text)), uint64(start), uint64(options))
	if err != nil {
		return nil, err
	}
	if result[0] == 0 {
		return nil, nil
	}
	mem := e.module.Memory()
	rp := uint32(result[0])
	index, _ := mem.ReadUint32Le(rp)
	count, _ := mem.ReadUint32Le(rp + 4)
	m := &Match{Index: int(index), Captures: make([]Capture, count)}
	for i := range m.Captures {
		a, _ := mem.ReadUint32Le(rp + 8 + uint32(i)*8)
		b, _ := mem.ReadUint32Le(rp + 12 + uint32(i)*8)
		m.Captures[i] = Capture{int(int32(a)), int(int32(b))}
	}
	return m, nil
}
func (s *Scanner) Close() error {
	e := s.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || s.ptr == 0 {
		return nil
	}
	_, err := e.destroy.Call(e.ctx, uint64(s.ptr))
	s.ptr = 0
	return err
}
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	return e.runtime.Close(e.ctx)
}
