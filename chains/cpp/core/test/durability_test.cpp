// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// durability_test.cpp — the only proof of durability that counts.
//
// Every other test of a store closes it politely first, and a store that only
// survives a clean shutdown is not durable: it is a cache with good manners.
// The failure this guards against is real and has been seen — a set that
// forgot itself on restart let the same thing be spent twice.
//
// So this test does not close anything. It starts a SECOND PROCESS, has it
// write and commit, and then has the kernel destroy it with SIGKILL: no
// destructor, no fclose, no atexit, no chance to flush. A third process then
// opens the same path and must find:
//
//   1. every committed row, byte for byte;
//   2. no uncommitted row at all;
//   3. the same ROOT — one hash folded over the whole set in key order, which
//      is the shape of the commitment a validator signs. Two stores that agree
//      row by row but fold to different roots would sign different blocks, so
//      the root is checked rather than assumed from the rows.
//
// The child is re-EXECed rather than merely forked, so it runs in a fresh
// address space: a page the parent had already written would otherwise be
// doing the store's job for it.

#include "lux/core/check.hpp"
#include "lux/core/store.hpp"

#include <sys/wait.h>
#include <unistd.h>

#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

using namespace lux::core;
using namespace lux::core::test;

namespace {

Bytes b(const std::string& s) { return Bytes(s.begin(), s.end()); }

// How many rows the child writes. Enough that a partial log would show up as a
// wrong root rather than as luck.
constexpr int kRows = 500;

std::string key_at(int i) {
    char t[32];
    std::snprintf(t, sizeof(t), "u%06d", i);
    return t;
}
std::string value_at(int i) {
    char t[64];
    std::snprintf(t, sizeof(t), "value-%d-%d", i, i * 7919);
    return t;
}

// The root: sha256 over the ordered walk of the whole set. This is the fold a
// chain's state root is, reduced to what a store alone can promise — so if the
// walk order or a single byte differs, this differs.
Id root_of(const store::Store& s) {
    Bytes acc;
    s.each({}, [&](ByteView key, ByteView value) {
        const std::uint32_t kn = std::uint32_t(key.size());
        const std::uint32_t vn = std::uint32_t(value.size());
        for (int i = 0; i < 4; ++i) acc.push_back(std::uint8_t(kn >> (8 * i)));
        acc.insert(acc.end(), key.begin(), key.end());
        for (int i = 0; i < 4; ++i) acc.push_back(std::uint8_t(vn >> (8 * i)));
        acc.insert(acc.end(), value.begin(), value.end());
        return true;
    });
    return sha256(view(acc));
}

// ── the child: write, commit, and die where it stands ────────────────────────

int child_write(const std::string& path) {
    auto f = store::File::open(path);
    if (!f) {
        std::fprintf(stderr, "child: %s\n", f.error().c_str());
        return 2;
    }
    for (int i = 0; i < kRows; ++i) {
        const Bytes k = b(key_at(i)), v = b(value_at(i));
        (*f)->put(view(k), view(v));
    }
    // Erasures are part of the state too: an absence that does not survive is
    // as wrong as a value that does not.
    for (int i = 0; i < kRows; i += 10) {
        const Bytes k = b(key_at(i));
        (*f)->erase(view(k));
    }
    if (auto r = (*f)->commit(); !r) {
        std::fprintf(stderr, "child: %s\n", r.error().c_str());
        return 2;
    }

    // Print the root the child saw, so the parent compares against what the
    // WRITER believed, not merely against its own second reading.
    std::printf("%s\n", hex(root_of(**f)).c_str());
    std::fflush(stdout);

    // Everything below this line must NOT survive: it is written and never
    // committed, which is exactly the state a node is in when a block is
    // half-verified and the machine goes away.
    for (int i = 0; i < kRows; ++i) {
        const Bytes k = b("doomed-" + key_at(i)), v = b("must not survive");
        (*f)->put(view(k), view(v));
    }

    // No return, no unwinding, no destructor. The kernel takes it from here.
    ::raise(SIGKILL);
    return 3;  // unreachable; a non-zero code in case a signal was blocked
}

// ── the parent: run the child, kill-check it, then read the disk ─────────────

// run_child re-execs this binary as `--write <path>` and returns the root the
// child printed, or "" if the child did not get that far.
std::string run_child(const std::string& self, const std::string& path, bool& killed) {
    killed = false;
    int pipefd[2];
    if (::pipe(pipefd) != 0) return {};

    const pid_t pid = ::fork();
    if (pid < 0) {
        ::close(pipefd[0]);
        ::close(pipefd[1]);
        return {};
    }
    if (pid == 0) {
        ::close(pipefd[0]);
        ::dup2(pipefd[1], STDOUT_FILENO);
        ::close(pipefd[1]);
        ::execl(self.c_str(), self.c_str(), "--write", path.c_str(), nullptr);
        ::_exit(127);
    }

    ::close(pipefd[1]);
    std::string out;
    char buf[256];
    ssize_t n;
    while ((n = ::read(pipefd[0], buf, sizeof(buf))) > 0) out.append(buf, std::size_t(n));
    ::close(pipefd[0]);

    int status = 0;
    ::waitpid(pid, &status, 0);
    killed = WIFSIGNALED(status) && WTERMSIG(status) == SIGKILL;

    while (!out.empty() && (out.back() == '\n' || out.back() == '\r')) out.pop_back();
    return out;
}

void a_killed_process_leaves_a_readable_store(const std::string& self) {
    const std::string path =
        "/tmp/lux-core-durability-" + std::to_string(::getpid()) + ".log";
    ::unlink(path.c_str());

    bool killed = false;
    const std::string child_root = run_child(self, path, killed);

    check(killed, "the writer was destroyed by SIGKILL, not shut down");
    check(!child_root.empty(), "and it had committed before it died");
    if (!killed || child_root.empty()) {
        ::unlink(path.c_str());
        return;
    }

    auto f = store::File::open(path);
    check(f.has_value(), "a new process opens the store the dead one left");
    if (!f) {
        ::unlink(path.c_str());
        return;
    }

    int missing = 0, wrong = 0, resurrected = 0, doomed = 0;
    for (int i = 0; i < kRows; ++i) {
        const Bytes k = b(key_at(i));
        auto got = (*f)->get(view(k));
        if (i % 10 == 0) {
            if (got.has_value()) ++resurrected;
            continue;
        }
        if (!got.has_value()) {
            ++missing;
            continue;
        }
        const Bytes want = b(value_at(i));
        if (*got != want) ++wrong;
    }
    for (int i = 0; i < kRows; ++i) {
        const Bytes k = b("doomed-" + key_at(i));
        if ((*f)->get(view(k)).has_value()) ++doomed;
    }

    check(missing == 0, "every committed row came back");
    check(wrong == 0, "and came back byte for byte");
    check(resurrected == 0, "an erase committed before the kill stayed an erase");
    check(doomed == 0, "and nothing written after the last commit survived");
    check((*f)->rows() == std::size_t(kRows - (kRows + 9) / 10),
          "the set holds exactly what was committed and nothing more");

    check_eq(hex(root_of(**f)), child_root,
             "the state root the reader folds is the one the writer signed");

    // Reading it a third time must give the same answer: a store that recovers
    // differently on the second open recovers by accident.
    {
        auto g = store::File::open(path);
        check(g.has_value(), "and it opens again");
        if (g) check_eq(hex(root_of(**g)), child_root, "with the same root a third time");
    }

    ::unlink(path.c_str());
}

}  // namespace

int main(int argc, char** argv) {
    if (argc == 3 && std::strcmp(argv[1], "--write") == 0) return child_write(argv[2]);

    std::printf("durability\n");
    // /proc/self/exe is the binary itself whatever the working directory or
    // the name it was invoked under.
    a_killed_process_leaves_a_readable_store("/proc/self/exe");
    return report("durability");
}
