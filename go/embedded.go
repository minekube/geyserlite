// SPDX-License-Identifier: MIT
package geyserlite

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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

// geyserAPI is the typed view of libgeyserlite.so's @CEntryPoint exports
// plus the GraalVM-provided isolate runtime.
type geyserAPI struct {
	createIsolate func(params uintptr, isolatePtr *uintptr, threadPtr *uintptr) int32 // graal_create_isolate
	tearDown      func(thread uintptr) int32                                          // graal_tear_down_isolate
	attachThread  func(isolate uintptr, threadPtr *uintptr) int32                     // graal_attach_thread
	detachThread  func(thread uintptr) int32                                          // graal_detach_thread
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

	// GraalVM's IsolateThread* is thread-affine: every isolate call
	// must come from the OS thread that the thread handle was minted
	// on. Go goroutines aren't pinned to OS threads by default, so we
	// dedicate one OS-locked goroutine to the create/init/run chain
	// and use graal_attach_thread for any cross-thread calls
	// (shutdown, status). Without this, GraalVM raises a
	// StackOverflowError on the first call from a stranger thread —
	// it interprets the wrong-thread call as runaway native recursion.
	configPath := filepath.Join(workdir, "config.yml")
	cstr, free := pinString(configPath)
	defer free()

	type isolateRefs struct {
		isolate uintptr
		thread  uintptr
	}
	createDone := make(chan isolateRefs, 1)
	createErr := make(chan error, 1)
	runDone := make(chan int32, 1)

	go func() {
		runtime.LockOSThread()
		// We never UnlockOSThread: when this goroutine exits, the
		// runtime kills its M anyway, which is the right behavior for
		// an isolate that's tearing down.

		var refs isolateRefs
		if rc := r.api.createIsolate(0, &refs.isolate, &refs.thread); rc != 0 {
			createErr <- fmt.Errorf("geyserlite: graal_create_isolate failed: rc=%d", rc)
			return
		}
		defer r.api.tearDown(refs.thread)

		if rc := r.api.init(refs.thread, cstr); rc != 0 {
			createErr <- fmt.Errorf("geyserlite: geyser_init failed: rc=%d", rc)
			return
		}
		createDone <- refs

		// run blocks until shutdown(); the isolate's main thread is
		// pinned to this goroutine for the duration.
		runDone <- r.api.run(refs.thread)
	}()

	var refs isolateRefs
	select {
	case refs = <-createDone:
	case err := <-createErr:
		return err
	case <-ctx.Done():
		// Cancellation before init finished — we have nothing to clean up.
		return ctx.Err()
	}

	// Health polling runs on its own attached thread. Status checks
	// from any other OS thread would land on the wrong IsolateThread*.
	go r.pollHealth(ctx, refs.isolate)

	defer r.healthyFlag.Store(false)

	select {
	case rc := <-runDone:
		if rc != 0 {
			return fmt.Errorf("geyserlite: geyser_run returned rc=%d", rc)
		}
		return nil
	case <-ctx.Done():
		// Request graceful shutdown — has to come from a freshly
		// attached thread, not the run goroutine (which is blocked
		// inside run()).
		if err := r.callOnAttachedThread(refs.isolate, func(thread uintptr) error {
			if rc := r.api.shutdown(thread); rc != 0 {
				return fmt.Errorf("geyser_shutdown rc=%d", rc)
			}
			return nil
		}); err != nil {
			s.logger.Warn("graceful shutdown failed", slog.String("err", err.Error()))
		}
		<-runDone
		return ctx.Err()
	}
}

// pollHealth attaches its own thread to the isolate and calls
// geyser_status on it until status reports up (1) or ctx is canceled.
// It receives the isolate handle, not a thread handle, because every
// goroutine that wants to call into GraalVM needs its own thread
// minted via graal_attach_thread.
func (r *embeddedRunner) pollHealth(ctx context.Context, isolate uintptr) {
	runtime.LockOSThread()
	// No UnlockOSThread: the goroutine ends with the polling.

	var thread uintptr
	if rc := r.api.attachThread(isolate, &thread); rc != 0 {
		// Without an attached thread we can't poll — bail out
		// silently; the run goroutine will still flip healthy via
		// other signals (or callers can fall back on time-based heuristics).
		return
	}
	defer r.api.detachThread(thread)

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
		select {
		case <-ctx.Done():
			return
		case <-pollSleep():
		}
	}
}

// callOnAttachedThread runs fn on a freshly-attached thread to the
// isolate, then detaches. Used for one-shot cross-thread calls like
// shutdown — anything called from a goroutine that doesn't own a
// thread handle of its own.
func (r *embeddedRunner) callOnAttachedThread(isolate uintptr, fn func(thread uintptr) error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		// No UnlockOSThread: detach + goroutine exit makes that moot.

		var thread uintptr
		if rc := r.api.attachThread(isolate, &thread); rc != 0 {
			done <- fmt.Errorf("graal_attach_thread rc=%d", rc)
			return
		}
		defer r.api.detachThread(thread)
		done <- fn(thread)
	}()
	return <-done
}

func (r *embeddedRunner) load(libpath string) error {
	var loadErr error
	r.once.Do(func() {
		lib, err := openLibrary(libpath)
		if err != nil {
			loadErr = fmt.Errorf("geyserlite: load library %s: %w", libpath, err)
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
		// graal_* come from the GraalVM runtime that's linked into
		// every shared library — no project-specific @CEntryPoint
		// needed. The host owns the isolate lifecycle here; the
		// geyser_* methods all run inside that isolate.
		bind(&r.api.createIsolate, "graal_create_isolate")
		bind(&r.api.tearDown, "graal_tear_down_isolate")
		bind(&r.api.attachThread, "graal_attach_thread")
		bind(&r.api.detachThread, "graal_detach_thread")
		bind(&r.api.init, "geyser_init")
		bind(&r.api.run, "geyser_run")
		bind(&r.api.shutdown, "geyser_shutdown")
		bind(&r.api.status, "geyser_status")
	})
	return loadErr
}

// cString lives in embedded_helpers.go as pinString. The old cString
// returned a raw uintptr into unpinned Go memory, which was GC-unsafe;
// pinString pins the backing array via runtime.Pinner so libgeyserlite
// can read it without the GC moving or reclaiming it first.
