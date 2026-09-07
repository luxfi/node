// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The optional backend: the kernel library, opened at run time if it is there.
//!
//! No kernel source lives in this repository and none ever will. What lives
//! here is four symbol names and their C signatures — the plugin ABI, which is
//! the whole point of having one. A node built and run with nothing installed
//! never opens anything and is not a lesser node; see [`crate::cpu`].

use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_int, c_void};
use std::sync::{Mutex, OnceLock};

/// `LuxError`: 0 is success. Any other value means "I did not answer", and the
/// caller falls back to the CPU rather than propagating a device's bad day into
/// consensus.
const LUX_OK: c_int = 0;

type Create = unsafe extern "C" fn() -> *mut c_void;
type Destroy = unsafe extern "C" fn(*mut c_void);
type BackendName = unsafe extern "C" fn(*mut c_void) -> *const c_char;
type Keccak256Batch =
    unsafe extern "C" fn(*mut c_void, *const u8, *mut u8, *const usize, usize) -> c_int;

extern "C" {
    fn dlopen(filename: *const c_char, flag: c_int) -> *mut c_void;
    fn dlsym(handle: *mut c_void, symbol: *const c_char) -> *mut c_void;
}

const RTLD_NOW: c_int = 2;
const RTLD_LOCAL: c_int = 0;

/// A live handle on the kernel library plus the one context it hands out.
struct Plugin {
    ctx: *mut c_void,
    destroy: Destroy,
    name: String,
    keccak256_batch: Keccak256Batch,
}

// The context is only ever touched under the mutex below, and the library
// stays mapped for the life of the process.
unsafe impl Send for Plugin {}

impl Drop for Plugin {
    fn drop(&mut self) {
        unsafe { (self.destroy)(self.ctx) }
    }
}

fn sym(handle: *mut c_void, name: &str) -> Option<*mut c_void> {
    let c = CString::new(name).ok()?;
    let p = unsafe { dlsym(handle, c.as_ptr()) };
    if p.is_null() {
        None
    } else {
        Some(p)
    }
}

/// The library's file name. `LUX_GPU_LIB` names an explicit path; otherwise the
/// platform soname is handed to the dynamic loader, which is the only
/// installed-software question this repository is entitled to ask.
fn candidates() -> Vec<String> {
    let mut v = Vec::new();
    if let Ok(p) = std::env::var("LUX_GPU_LIB") {
        if !p.is_empty() {
            v.push(p);
        }
    }
    v.push(
        if cfg!(target_os = "macos") {
            "libluxgpu.dylib"
        } else {
            "libluxgpu.so"
        }
        .to_string(),
    );
    v
}

fn open() -> Option<Plugin> {
    for path in candidates() {
        let c = match CString::new(path.as_str()) {
            Ok(c) => c,
            Err(_) => continue,
        };
        let handle = unsafe { dlopen(c.as_ptr(), RTLD_NOW | RTLD_LOCAL) };
        if handle.is_null() {
            continue;
        }
        let (create, destroy, name_fn, keccak) = match (
            sym(handle, "lux_gpu_create"),
            sym(handle, "lux_gpu_destroy"),
            sym(handle, "lux_gpu_backend_name"),
            sym(handle, "lux_gpu_keccak256_batch"),
        ) {
            (Some(a), Some(b), Some(c), Some(d)) => (a, b, c, d),
            // A library that answers to the name but not to the ABI is not a
            // backend. Refusing it is what keeps a half-loaded plugin from
            // looking like a working one.
            _ => continue,
        };
        let create: Create = unsafe { std::mem::transmute(create) };
        let destroy: Destroy = unsafe { std::mem::transmute(destroy) };
        let name_fn: BackendName = unsafe { std::mem::transmute(name_fn) };
        let keccak256_batch: Keccak256Batch = unsafe { std::mem::transmute(keccak) };

        let ctx = unsafe { create() };
        if ctx.is_null() {
            continue;
        }
        let name = unsafe {
            let p = name_fn(ctx);
            if p.is_null() {
                "unknown".to_string()
            } else {
                CStr::from_ptr(p).to_string_lossy().into_owned()
            }
        };
        return Some(Plugin {
            ctx,
            destroy,
            name,
            keccak256_batch,
        });
    }
    None
}

fn plugin() -> Option<&'static Mutex<Plugin>> {
    static P: OnceLock<Option<Mutex<Plugin>>> = OnceLock::new();
    P.get_or_init(|| open().map(Mutex::new)).as_ref()
}

/// The live backend's own name (`cpu`, `cuda`, `metal`, ...), or `None` when no
/// library is installed.
pub fn backend_name() -> Option<String> {
    let p = plugin()?;
    let g = p.lock().ok()?;
    Some(g.name.clone())
}

/// One Keccak-256 per input, through the plugin. `None` means "ask the CPU" —
/// no library, a locked-out mutex, or a backend that declined the batch.
pub fn keccak256_batch(inputs: &[&[u8]]) -> Option<Vec<crate::Hash256>> {
    let p = plugin()?;
    let g = p.lock().ok()?;

    // A zero-length batch has no answer to ask for, and the C side is entitled
    // to reject the pointers an empty batch would hand it.
    if inputs.is_empty() {
        return Some(Vec::new());
    }

    let total: usize = inputs.iter().map(|i| i.len()).sum();
    // A batch of nothing but empty inputs is legal — keccak of the empty string
    // is a real value — so the buffer still has to be a pointer the C side can
    // hold, even though it will read no bytes through it.
    let mut flat = Vec::with_capacity(total.max(1));
    let mut lens = Vec::with_capacity(inputs.len());
    for i in inputs {
        flat.extend_from_slice(i);
        lens.push(i.len());
    }
    let mut out = vec![0u8; inputs.len() * 32];

    let rc = unsafe {
        (g.keccak256_batch)(
            g.ctx,
            flat.as_ptr(),
            out.as_mut_ptr(),
            lens.as_ptr(),
            inputs.len(),
        )
    };
    if rc != LUX_OK {
        return None;
    }
    let mut digests = vec![[0u8; 32]; inputs.len()];
    for (i, d) in digests.iter_mut().enumerate() {
        d.copy_from_slice(&out[i * 32..(i + 1) * 32]);
    }
    Some(digests)
}
