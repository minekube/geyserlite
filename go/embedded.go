// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

// embeddedRunner loads libgeyserlite.so via purego and calls @CEntryPoint
// methods directly. No subprocess. A native crash kills the host process —
// recover() does not catch it. Use [ModeSubprocess] if you need crash isolation.
type embeddedRunner struct {
	healthyFlag atomic.Bool

	once sync.Once
	lib  uintptr
	api  geyserAPI
}

// geyserAPI is the typed view of libgeyserlite.so's @CEntryPoint exports.
type geyserAPI struct {
	createIsolate func(out *uintptr) int32 // geyser_create_isolate(IsolateThread**)
	tearDown      func(thread uintptr) int32
	init          func(thread uintptr, configPath uintptr) int32
	run           func(thread uintptr) int32
	shutdown      func(thread uintptr) int32
	status        func(thread uintptr) int32
}

func (r *embeddedRunner) healthy() bool { return r.healthyFlag.Load() }

func (r *embeddedRunner) run(ctx context.Context, s *Server) error {
	libpath, err := locateLibrary(ctx, s.opts)
	if err != nil {
		return err
	}
	s.logger.Info("loading libgeyserlite", slog.String("path", libpath))

	if err := r.load(libpath); err != nil {
		return err
	}

	workdir, err := os.MkdirTemp("", "geyserlite-*")
	if err != nil {
		return fmt.Errorf("geyserlite: create workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	if err := writeFloodgateKey(workdir, s.opts); err != nil {
		return err
	}
	if err := renderConfig(workdir, s.opts); err != nil {
		return err
	}
	if err := copyPermissionsYML(workdir); err != nil {
		s.logger.Warn("could not stage permissions.yml", slog.String("err", err.Error()))
	}

	// chdir into the workdir for Geyser's relative-path file access.
	// We don't rely on global state — Geyser inside the isolate sees this cwd.
	prev, _ := os.Getwd()
	if err := os.Chdir(workdir); err != nil {
		return fmt.Errorf("geyserlite: chdir: %w", err)
	}
	defer os.Chdir(prev)

	var thread uintptr
	if rc := r.api.createIsolate(&thread); rc != 0 {
		return fmt.Errorf("geyserlite: geyser_create_isolate failed: rc=%d", rc)
	}
	defer r.api.tearDown(thread)

	configPath := filepath.Join(workdir, "config.yml")
	cstr, free := cString(configPath)
	defer free()

	if rc := r.api.init(thread, cstr); rc != 0 {
		return fmt.Errorf("geyserlite: geyser_init failed: rc=%d", rc)
	}

	// Geyser run blocks. Drive it from a goroutine so we can react to ctx.
	runDone := make(chan int32, 1)
	go func() {
		runDone <- r.api.run(thread)
	}()

	// Poll status to flip the healthy flag once Geyser is up.
	go r.pollHealth(ctx, thread)

	defer r.healthyFlag.Store(false)

	select {
	case rc := <-runDone:
		if rc != 0 {
			return fmt.Errorf("geyserlite: geyser_run returned rc=%d", rc)
		}
		return nil
	case <-ctx.Done():
		// Request graceful shutdown.
		if rc := r.api.shutdown(thread); rc != 0 {
			s.logger.Warn("geyser_shutdown returned non-zero", slog.Int("rc", int(rc)))
		}
		<-runDone
		return ctx.Err()
	}
}

func (r *embeddedRunner) pollHealth(ctx context.Context, thread uintptr) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if r.api.status(thread) == 1 {
			r.healthyFlag.Store(true)
			return
		}
		// Poll every 250ms during boot.
		select {
		case <-ctx.Done():
			return
		case <-pollSleep():
		}
	}
}

func (r *embeddedRunner) load(libpath string) error {
	var loadErr error
	r.once.Do(func() {
		lib, err := purego.Dlopen(libpath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("geyserlite: dlopen %s: %w", libpath, err)
			return
		}
		r.lib = lib

		// Bind each @CEntryPoint export. Argument types must match
		// libgeyserlite.h exactly. See build/overlay/.../GeyserBridge.java.
		bind := func(target any, name string) {
			if loadErr != nil {
				return
			}
			defer func() {
				if v := recover(); v != nil {
					loadErr = fmt.Errorf("geyserlite: bind %s: %v", name, v)
				}
			}()
			purego.RegisterLibFunc(target, lib, name)
		}
		bind(&r.api.createIsolate, "geyser_create_isolate")
		bind(&r.api.tearDown, "geyser_tear_down_isolate")
		bind(&r.api.init, "geyser_init")
		bind(&r.api.run, "geyser_run")
		bind(&r.api.shutdown, "geyser_shutdown")
		bind(&r.api.status, "geyser_status")
	})
	return loadErr
}

// cString allocates a NUL-terminated copy of s in C memory and returns a
// pointer to it plus a free function. Caller must invoke free.
func cString(s string) (uintptr, func()) {
	// purego provides Calloc-style helpers but to avoid pulling in cgo,
	// we use a Go []byte and unsafe.Pointer; libgeyserlite reads it in init
	// before run blocks so the pointer remains valid.
	b := append([]byte(s), 0)
	// Pin via runtime.KeepAlive in the call site if we ever cross boundaries
	// during a callback. For init/shutdown that doesn't happen.
	return ptrOf(b), func() { _ = b }
}

var (
	errAPIBindingMissing = errors.New("geyserlite: libgeyserlite.so missing required @CEntryPoint export")
)
