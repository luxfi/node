// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//go:build cgo

package gpu

// The optional backend: the kernel library, opened at run time if it is there.
//
// No kernel source lives in this repository and none ever will. What is
// declared below is four symbol names and their C signatures — the plugin ABI,
// which is the whole point of having one. Nothing is linked at build time: the
// library is dlopen'd, so a build machine that has never heard of it produces
// the same binary as one that has.

/*
#include <dlfcn.h>
#include <stdlib.h>

typedef void*       (*lux_create_t)(void);
typedef void        (*lux_destroy_t)(void*);
typedef const char* (*lux_name_t)(void*);
typedef int         (*lux_keccak_t)(void*, const unsigned char*, unsigned char*,
                                    const size_t*, size_t);

static void*       lux_call_create(void* fn)             { return ((lux_create_t)fn)(); }
static const char* lux_call_name(void* fn, void* ctx)    { return ((lux_name_t)fn)(ctx); }
static int         lux_call_keccak(void* fn, void* ctx,
                                   const unsigned char* in, unsigned char* out,
                                   const size_t* lens, size_t n) {
    return ((lux_keccak_t)fn)(ctx, in, out, lens, n);
}
*/
import "C"

import (
	"os"
	"runtime"
	"sync"
	"unsafe"
)

// luxOK is LuxError 0. Any other value means "I did not answer", and the caller
// falls back to the CPU rather than propagating a device's bad day into
// consensus.
const luxOK = 0

type library struct {
	ctx    unsafe.Pointer
	name   string
	keccak unsafe.Pointer
}

// One library, one context, opened once. Every call holds the mutex: the
// context's thread-safety is the plugin's business, not ours to assume.
var (
	openOnce sync.Once
	lib      *library
	libMu    sync.Mutex
)

// candidates is the library's file name. LUX_GPU_LIB names an explicit path;
// otherwise the platform soname is handed to the dynamic loader, which is the
// only installed-software question this repository is entitled to ask.
func candidates() []string {
	var v []string
	if p := os.Getenv("LUX_GPU_LIB"); p != "" {
		v = append(v, p)
	}
	if runtime.GOOS == "darwin" {
		return append(v, "libluxgpu.dylib")
	}
	return append(v, "libluxgpu.so")
}

func sym(h unsafe.Pointer, name string) unsafe.Pointer {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return C.dlsym(h, cname)
}

func open() *library {
	for _, path := range candidates() {
		cpath := C.CString(path)
		h := C.dlopen(cpath, C.RTLD_NOW|C.RTLD_LOCAL)
		C.free(unsafe.Pointer(cpath))
		if h == nil {
			continue
		}
		create := sym(h, "lux_gpu_create")
		nameOf := sym(h, "lux_gpu_backend_name")
		keccak := sym(h, "lux_gpu_keccak256_batch")
		// A library that answers to the name but not to the ABI is not a
		// backend. Refusing it keeps a half-loaded plugin from looking like a
		// working one. lux_gpu_destroy is required for the same reason even
		// though the context lives for the life of the process.
		if create == nil || nameOf == nil || keccak == nil || sym(h, "lux_gpu_destroy") == nil {
			continue
		}
		ctx := C.lux_call_create(create)
		if ctx == nil {
			continue
		}
		n := C.lux_call_name(nameOf, ctx)
		name := "unknown"
		if n != nil {
			name = C.GoString(n)
		}
		return &library{ctx: ctx, name: name, keccak: keccak}
	}
	return nil
}

func loaded() *library {
	openOnce.Do(func() { lib = open() })
	return lib
}

func pluginName() (string, bool) {
	libMu.Lock()
	defer libMu.Unlock()
	l := loaded()
	if l == nil {
		return "", false
	}
	return l.name, true
}

func pluginKeccak256Batch(inputs [][]byte) ([]Hash256, bool) {
	libMu.Lock()
	defer libMu.Unlock()
	l := loaded()
	if l == nil {
		return nil, false
	}
	// A zero-length batch has no answer to ask for, and the C side is entitled
	// to reject the pointers an empty batch would hand it.
	if len(inputs) == 0 {
		return []Hash256{}, true
	}

	total := 0
	for _, in := range inputs {
		total += len(in)
	}
	// A batch of nothing but empty inputs is legal — keccak of the empty string
	// is a real value — so the buffer still has to be a pointer the C side can
	// hold, even though it will read no bytes through it.
	flat := make([]byte, 0, max(total, 1))
	lens := make([]C.size_t, len(inputs))
	for i, in := range inputs {
		flat = append(flat, in...)
		lens[i] = C.size_t(len(in))
	}
	if len(flat) == 0 {
		flat = append(flat, 0)
	}
	out := make([]byte, len(inputs)*32)

	rc := C.lux_call_keccak(l.keccak, l.ctx,
		(*C.uchar)(unsafe.Pointer(&flat[0])),
		(*C.uchar)(unsafe.Pointer(&out[0])),
		(*C.size_t)(unsafe.Pointer(&lens[0])),
		C.size_t(len(inputs)))
	if rc != luxOK {
		return nil, false
	}

	digests := make([]Hash256, len(inputs))
	for i := range digests {
		copy(digests[i][:], out[i*32:(i+1)*32])
	}
	return digests, true
}
