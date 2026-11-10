// Host-side test of the sim_bus hub's 64 KiB TX cap (BUS-03): a simulator that stops reading must
// make the hub drop the connection, never block and never grow without bound.
//
// Compiles the shipped components/sim_bus/sim_bus.cpp (by path, never a copy) against the stub
// ESPHome headers in tests/cpp/stubs/ and talks to it over a real AF_UNIX socket in a private temp
// directory. Time is the stub clock esphome::stub::now_ms; the test never sleeps. alarm(20) kills
// the process if the hub ever blocks. This file must never live under components/: ESPHome compiles
// every .cpp it finds there.
//
// Build and run: make test-cpp (host g++), make test-cpp-image (image gcc).
#include "sim_bus.h"
#include "sim_protocol.h"

#include <fcntl.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

#include <algorithm>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

namespace sim_bus = esphome::sim_bus;
namespace proto = esphome::sim_bus::proto;

namespace {

int g_passed = 0;
int g_failed = 0;
std::vector<std::string> g_warnings;  // every W message the hub logged

void check(bool ok, const std::string &name, const std::string &why) {
  if (ok) {
    g_passed++;
  } else {
    g_failed++;
    std::printf("FAIL %s: %s\n", name.c_str(), why.c_str());
  }
}

}  // namespace

namespace esphome::stub {
void log(char level, const char *tag, const char *fmt, ...) {
  char buf[512];
  va_list ap;
  va_start(ap, fmt);
  std::vsnprintf(buf, sizeof(buf), fmt, ap);
  va_end(ap);
  std::printf("[%c][%s] %s\n", level, tag, buf);
  if (level == 'W') g_warnings.emplace_back(buf);
}
}  // namespace esphome::stub

namespace {

/// The hub with the members the test inspects made public.
struct TestBus : sim_bus::SimBus {
  using SimBus::backoff_;
  using SimBus::fd_;
  using SimBus::retry_delay_;
  using SimBus::try_connect_;
  using SimBus::tx_;
};

/// One output channel; counts what the hub tells its children.
struct TxChild : sim_bus::SimChild {
  int connected = 0;
  int disconnected = 0;
  const char *direction() const override { return "out"; }
  void on_connected() override { connected++; }
  void on_disconnected() override { disconnected++; }
};

/// Private temp dir with a listening socket; every fd, the socket file and the dir go on destruction.
struct Env {
  std::string dir;
  std::string sock;
  int listen_fd = -1;
  std::vector<int> peers;

  bool init() {
    char tmpl[] = "/tmp/siltxXXXXXX";
    if (mkdtemp(tmpl) == nullptr) return false;
    dir = tmpl;
    sock = dir + "/bus.sock";
    listen_fd = ::socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (listen_fd < 0) return false;
    sockaddr_un a{};
    a.sun_family = AF_UNIX;
    std::strncpy(a.sun_path, sock.c_str(), sizeof(a.sun_path) - 1);
    if (::bind(listen_fd, reinterpret_cast<sockaddr *>(&a), sizeof(a)) != 0) return false;
    return ::listen(listen_fd, 8) == 0;
  }
  /// Accepts the pending connection; the peer fd is non-blocking. -1 when none is pending.
  int accept_peer() {
    int fd = ::accept4(listen_fd, nullptr, nullptr, SOCK_NONBLOCK | SOCK_CLOEXEC);
    if (fd >= 0) peers.push_back(fd);
    return fd;
  }
  ~Env() {
    for (int fd : peers) ::close(fd);
    if (listen_fd >= 0) ::close(listen_fd);
    if (!sock.empty()) ::unlink(sock.c_str());
    if (!dir.empty()) ::rmdir(dir.c_str());
  }
};

struct Ctx {
  Env env;
  TestBus bus;
  TxChild child;
  int peer = -1;     // the accepted end of the current connection
  unsigned n = 0;    // set_out calls so far, across all stalls
  float last = 0.f;  // the last value handed to set_out
};

struct StallResult {
  unsigned calls = 0;
  size_t max_pending = 0;  // largest tx_ seen after a call that left the hub connected
  bool over_cap_while_connected = false;
};

/// Drives the hub with changing values while the peer does not read, until it drops the connection.
StallResult stall(Ctx &c) {
  StallResult r;
  // A UDS stream is limited by the sender's buffer: shrink the hub's, so the kernel backs up soon.
  int sz = 4096;
  ::setsockopt(c.bus.fd_, SOL_SOCKET, SO_SNDBUF, &sz, sizeof(sz));
  while (c.bus.connected() && r.calls < 200000) {
    c.last = 0.001f * static_cast<float>(++c.n);
    c.bus.set_out("port1.duty", c.last);
    r.calls++;
    if (c.bus.connected()) {
      r.max_pending = std::max(r.max_pending, c.bus.tx_.size());
      if (c.bus.tx_.size() > sim_bus::SimBus::kMaxTx) r.over_cap_while_connected = true;
    }
  }
  return r;
}

/// The cap warning logged at or after index `from` of g_warnings: how many, and the byte count of the first.
struct CapWarn {
  int count = 0;
  unsigned bytes = 0;
};
CapWarn cap_warnings(size_t from) {
  CapWarn w;
  const std::string needle = "bytes pending for a simulator that does not read; dropping the connection";
  for (size_t i = from; i < g_warnings.size(); i++) {
    if (g_warnings[i].find(needle) == std::string::npos) continue;
    if (w.count == 0) std::sscanf(g_warnings[i].c_str(), "sim bus: %u bytes pending", &w.bytes);
    w.count++;
  }
  return w;
}

void drop_above_cap(Ctx &c) {
  const char *name = "drop_above_cap";
  c.child.set_channel("port1.duty");
  c.bus.register_child(&c.child);
  c.bus.set_path(c.env.sock);
  c.bus.try_connect_();
  check(c.bus.connected(), name, "hub did not connect to the test listener");
  c.peer = c.env.accept_peer();
  check(c.peer >= 0, name, "no connection arrived at the listener");
  check(c.child.connected == 1, name, "child saw " + std::to_string(c.child.connected) + " on_connected, want 1");

  const size_t warn_from = g_warnings.size();
  const StallResult r = stall(c);
  const CapWarn w = cap_warnings(warn_from);
  std::printf("txcap: dropped after %u set_out calls, max pending while connected %zu, at the drop %u\n", r.calls,
              r.max_pending, w.bytes);

  check(!r.over_cap_while_connected, name, "pending TX exceeded kMaxTx while the hub was still connected");
  check(!c.bus.connected() && c.bus.fd_ == -1, name, "hub is still connected after 200000 calls");
  check(c.bus.tx_.empty(), name, "tx_ not empty after the drop: " + std::to_string(c.bus.tx_.size()));
  check(w.count == 1, name, "want exactly one cap warning, got " + std::to_string(w.count));
  check(w.bytes > sim_bus::SimBus::kMaxTx, name, "warned byte count " + std::to_string(w.bytes) + " is not above 65536");
  check(c.child.disconnected == 1, name,
        "child saw " + std::to_string(c.child.disconnected) + " on_disconnected, want 1");
  check(r.calls > 1, name, "the cap was hit by a single call");
}

}  // namespace

int main() {
  alarm(20);  // a blocking hub would hang here; SIGALRM turns that into a failed make
  Ctx c;
  if (!c.env.init()) {
    std::fprintf(stderr, "cannot set up the test socket\n");
    return 2;
  }
  drop_above_cap(c);

  std::printf("txcap: %d passed, %d failed\n", g_passed, g_failed);
  return g_failed == 0 ? 0 : 1;
}
