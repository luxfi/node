// Package platformvm GPU backend selection — pure-Go shim that probes the
// lux-gpu-kernels plugin DSOs at PROCESS START in the canonical order
// (cuda → hip → metal → vulkan → webgpu) and exposes the first successful
// open as ActiveGPUBackend().
//
// Decomplected from platformvm_gpu.go / platformvm_gpu_nocgo.go: the
// probe order and package-level handle live here. The actual dlopen +
// dlsym calls live in platformvm_gpu.go (cgo build) or in the nocgo
// stub. Both files expose the same openGPUBackend signature so this
// file compiles identically in either mode.
//
// If a plugin opens cleanly, the chain routes transitions through it.
// If a probe fails (no plugin installed, missing runtime, etc.) the
// chain runs the existing pure-Go path. Every GPU launcher error makes
// the caller fall back to the Go path WITHOUT panic — strict positive
// overlay with graceful degradation.
package platformvm

import (
	"os"
	"runtime"
	"sync"
)

// =============================================================================
// Canonical plugin search paths — every backend has a name + a list of
// DSO basenames the loader tries in order. The first one that opens AND
// dlsym's all four launchers wins.
// =============================================================================

type pluginCandidate struct {
	kind      GPUBackendKind
	basenames []string
}

// candidatePlugins is the dlopen probe order: cuda → hip → metal → vulkan
// → webgpu. Each candidate lists the DSO basenames the loader probes (no
// path component). The probe layers add platform-conventional prefix +
// suffix (lib*.so / lib*.dylib).
func candidatePlugins() []pluginCandidate {
	return []pluginCandidate{
		{GPUCUDA, []string{
			"libluxgpu_backend_cuda.so",
			"libluxgpu_backend_cuda.dylib",
		}},
		{GPUHIP, []string{
			"libluxgpu_backend_hip.so",
			"libluxgpu_backend_hip.dylib",
		}},
		{GPUMetal, []string{
			"libluxgpu_backend_metal.dylib",
		}},
		{GPUVulkan, []string{
			"libluxgpu_backend_vulkan.so",
			"libluxgpu_backend_vulkan.dylib",
		}},
		{GPUWebGPU, []string{
			"libluxgpu_backend_webgpu.so",
			"libluxgpu_backend_webgpu.dylib",
		}},
	}
}

// searchPaths returns the directories the loader probes for each
// candidate basename. Order:
//
//  1. LUX_GPU_PLUGIN_DIR — shared with every other VM (aivm, bridgevm, …).
//  2. ~/work/lux-private/gpu-kernels/build/<flavor>_backend/ — dev tree.
//  3. /usr/local/lib/lux-gpu/ — prefix install.
//  4. /usr/lib/lux-gpu/.
//  5. Empty string ("" — let dlopen consult the system search path).
//
// The empty-string fallback lets a plugin in DYLD_LIBRARY_PATH /
// LD_LIBRARY_PATH still resolve, which is the standard CI override.
func searchPaths() []string {
	out := make([]string, 0, 8)
	if d := os.Getenv("LUX_GPU_PLUGIN_DIR"); d != "" {
		out = append(out, d)
	}
	if home, err := os.UserHomeDir(); err == nil {
		// Match the lux-private/gpu-kernels Cmake out-of-tree
		// convention: each backend lands in its own subdir.
		out = append(out,
			home+"/work/lux-private/gpu-kernels/build/cuda_backend",
			home+"/work/lux-private/gpu-kernels/build/hip_backend",
			home+"/work/lux-private/gpu-kernels/build/metal_backend",
			home+"/work/lux-private/gpu-kernels/build/vulkan_backend",
			home+"/work/lux-private/gpu-kernels/build/webgpu_backend",
		)
	}
	out = append(out, "/usr/local/lib/lux-gpu", "/usr/lib/lux-gpu", "")
	return out
}

// =============================================================================
// Package-level active backend — set once by init(), read by every call site.
// =============================================================================

var (
	activeMu      sync.RWMutex
	activeBackend *GPUBackend // nil = no plugin loaded
)

// ActiveGPUBackend returns the currently-loaded backend, or nil if none
// satisfied the probe at init(). Callers MUST check b.IsAvailable() before
// invoking any kernel; calling on a nil receiver is safe and returns
// ErrGPUNotAvailable from every method.
func ActiveGPUBackend() *GPUBackend {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return activeBackend
}

// SetActiveGPUBackend replaces the active backend. Used by tests to inject
// a mock plugin. Returns the previous backend (caller is responsible for
// closing it). Passing nil clears the slot.
func SetActiveGPUBackend(b *GPUBackend) *GPUBackend {
	activeMu.Lock()
	defer activeMu.Unlock()
	prev := activeBackend
	activeBackend = b
	return prev
}

// =============================================================================
// AutoBackend — pure-Go probe driver. Walks candidatePlugins() in order and
// returns the first one that opens cleanly. Returns nil if every probe fails
// (no plugin installed, missing runtime, etc.).
// =============================================================================

// AutoBackend probes the canonical plugin search paths in the cuda →
// hip → metal → vulkan → webgpu order and returns the first backend that
// opens AND resolves all four host launchers. Returns nil + the last
// error if no plugin satisfies the probe.
func AutoBackend() (*GPUBackend, error) {
	var lastErr error = ErrGPUNotAvailable
	for _, cand := range candidatePlugins() {
		// Skip kind-incompatible probes early. Metal only on darwin,
		// cuda/hip primarily on linux. The wrong-platform DSOs simply
		// won't be on disk — but skipping known impossibles makes the
		// fail-fast logs less noisy.
		if !kindLikelyOnHost(cand.kind) {
			continue
		}
		for _, basename := range cand.basenames {
			for _, dir := range searchPaths() {
				path := basename
				if dir != "" {
					path = dir + "/" + basename
				}
				b, err := openGPUBackend(cand.kind, path)
				if err != nil {
					lastErr = err
					continue
				}
				return b, nil
			}
		}
	}
	return nil, lastErr
}

// kindLikelyOnHost is a coarse platform filter — true if the OS could
// plausibly host this backend's runtime. The dlopen call would catch
// impossibilities anyway, but skipping known-bad combos cuts noise.
func kindLikelyOnHost(k GPUBackendKind) bool {
	switch k {
	case GPUMetal:
		return runtime.GOOS == "darwin"
	case GPUCUDA, GPUHIP:
		// CUDA/HIP are linux-first; the linux runtime may also be
		// loadable under WSL2. macOS cannot run either today.
		return runtime.GOOS != "darwin"
	case GPUVulkan, GPUWebGPU:
		// Vulkan via MoltenVK works on darwin too; WebGPU via Dawn/wgpu
		// is portable.
		return true
	default:
		return false
	}
}

// =============================================================================
// init() — runs the canonical probe order at process start. The result is
// stored in the package-level activeBackend slot. Any error is silently
// swallowed (the chain runs the Go path) so a missing plugin never
// blocks node startup.
//
// Test code can call SetActiveGPUBackend() after init() to override.
// =============================================================================

func init() {
	b, err := AutoBackend()
	if err != nil || b == nil {
		// Silent — the absence of a plugin is the common case.
		return
	}
	activeMu.Lock()
	activeBackend = b
	activeMu.Unlock()
}
