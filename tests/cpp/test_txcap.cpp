// Host-side test of the sim_bus hub's 64 KiB TX cap (BUS-03): a simulator that stops reading must
// make the hub drop the connection, never block and never grow without bound; the hub then
// reconnects with backoff (100 ms, 200 ms for a session that got no welcome, 100 ms again after a
// welcome) and replays hello, the channel manifest, the latest output value and sync.
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
#include "esphome/core/hal.h"

#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

#include <algorithm>
#include <csignal>
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

// The watchdog's SIGALRM kills the process before Env's destructor can run, so the paths it must
// remove are kept in static buffers the signal handler can reach. Only async-signal-safe calls
// (unlink, rmdir, _exit) may run there: no std::string, no stdio, no allocation.
char g_cleanup_sock[128];
char g_cleanup_dir[128];

void on_watchdog(int) {
  if (g_cleanup_sock[0] != '\0') ::unlink(g_cleanup_sock);
  if (g_cleanup_dir[0] != '\0') ::rmdir(g_cleanup_dir);
  ::_exit(3);
}

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
    if (dir.size() < sizeof(g_cleanup_dir) && sock.size() < sizeof(g_cleanup_sock)) {
      std::memcpy(g_cleanup_dir, dir.c_str(), dir.size() + 1);
      std::memcpy(g_cleanup_sock, sock.c_str(), sock.size() + 1);
    }
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
  /// Closes a peer end (the test's side of a connection) and forgets it.
  void release_peer(int fd) {
    if (fd < 0) return;
    ::close(fd);
    for (size_t i = 0; i < peers.size(); i++) {
      if (peers[i] == fd) {
        peers.erase(peers.begin() + static_cast<long>(i));
        break;
      }
    }
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
  uint32_t drop_at = 0;  // stub clock when the hub dropped the connection (close_ stamps it as last_try_)
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
  check(::setsockopt(c.bus.fd_, SOL_SOCKET, SO_SNDBUF, &sz, sizeof(sz)) == 0, "stall",
        "setsockopt(SO_SNDBUF) failed, the stall loop would not back the kernel up");
  while (c.bus.connected() && r.calls < 200000) {
    c.last = 0.001f * static_cast<float>(++c.n);
    c.bus.set_out("port1.duty", c.last);
    r.calls++;
    if (c.bus.connected()) {
      r.max_pending = std::max(r.max_pending, c.bus.tx_.size());
      if (c.bus.tx_.size() > sim_bus::SimBus::kMaxTx) r.over_cap_while_connected = true;
    }
  }
  c.drop_at = esphome::stub::now_ms;
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

/// One stall to the drop, with the checks every drop must pass. The expected delays are literals
/// taken from the rules in sim_bus.cpp, never read back from the bus.
void expect_drop(Ctx &c, const char *name, int want_disconnects, uint32_t want_retry, uint32_t want_backoff) {
  const size_t warn_from = g_warnings.size();
  const StallResult r = stall(c);
  const CapWarn w = cap_warnings(warn_from);
  std::printf("txcap: %s: dropped after %u set_out calls, max pending while connected %zu, at the drop %u\n", name,
              r.calls, r.max_pending, w.bytes);

  check(!r.over_cap_while_connected, name, "pending TX exceeded kMaxTx while the hub was still connected");
  check(!c.bus.connected() && c.bus.fd_ == -1, name, "hub is still connected after 200000 calls");
  check(c.bus.tx_.empty(), name, "tx_ not empty after the drop: " + std::to_string(c.bus.tx_.size()));
  check(w.count == 1, name, "want exactly one cap warning, got " + std::to_string(w.count));
  check(w.bytes > sim_bus::SimBus::kMaxTx, name, "warned byte count " + std::to_string(w.bytes) + " is not above 65536");
  check(c.child.disconnected == want_disconnects, name,
        "child saw " + std::to_string(c.child.disconnected) + " on_disconnected, want " + std::to_string(want_disconnects));
  check(r.calls > 1, name, "the cap was hit by a single call");
  check(c.bus.retry_delay_ == want_retry, name,
        "retry_delay_ " + std::to_string(c.bus.retry_delay_) + ", want " + std::to_string(want_retry));
  check(c.bus.backoff_ == want_backoff, name,
        "backoff_ " + std::to_string(c.bus.backoff_) + ", want " + std::to_string(want_backoff));
}

/// Everything the peer can read right now, split into lines; false when a line does not parse.
struct Replay {
  std::vector<std::string> lines;
  std::vector<proto::Frame> frames;
  bool all_parse = true;
};
Replay read_replay(int fd) {
  Replay r;
  std::string data;
  char buf[1024];
  for (;;) {
    ssize_t n = ::recv(fd, buf, sizeof(buf), MSG_DONTWAIT);
    if (n <= 0) break;  // EAGAIN: nothing more queued
    data.append(buf, static_cast<size_t>(n));
  }
  size_t start = 0;
  for (size_t nl; (nl = data.find('\n', start)) != std::string::npos; start = nl + 1) {
    r.lines.push_back(data.substr(start, nl - start));
    proto::Frame f;
    if (proto::parse(r.lines.back(), f) != proto::Err::NONE) r.all_parse = false;
    r.frames.push_back(f);
  }
  return r;
}

/// The replay a reconnect must carry: hello (proto 1), the chan frame, the latest value, sync last.
void check_replay(Ctx &c, const char *name) {
  const Replay rp = read_replay(c.peer);
  check(rp.all_parse, name, "a replay line does not parse with proto::parse");
  check(rp.frames.size() == 4, name, "replay has " + std::to_string(rp.frames.size()) + " lines, want 4");
  if (rp.frames.size() != 4) return;
  int64_t p = 0;
  check(rp.frames[0].kind() == "hello" && rp.frames[0].get_int("proto", p) && p == 1, name,
        "first replay line is not hello with proto 1: " + rp.lines[0]);
  std::string ch;
  check(rp.frames[1].kind() == "chan" && rp.frames[1].get_str("ch", ch) && ch == "port1.duty", name,
        "second replay line is not chan port1.duty: " + rp.lines[1]);
  float v = 0.f;
  check(rp.frames[2].kind() == "out" && rp.frames[2].get_str("ch", ch) && ch == "port1.duty" &&
            rp.frames[2].get_num("v", v) && v == c.last,
        name, "third replay line is not out port1.duty with the latest value: " + rp.lines[2]);
  check(rp.frames[3].kind() == "sync", name, "last replay line is not sync: " + rp.lines[3]);
}

/// Advances the stub clock to `drop_at + after_ms`, runs the hub's loop once and returns whether it is connected.
bool loop_at(Ctx &c, uint32_t after_ms) {
  esphome::stub::now_ms = c.drop_at + after_ms;
  c.bus.loop();
  return c.bus.connected();
}

// Drop 1 is a session that never got a welcome: retry_delay_ 100, backoff_ doubles to 200.
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

  expect_drop(c, name, 1, 100, 200);
}

// No new drop: connection 2 comes back only once the 100 ms retry delay has passed, with a full replay.
void reconnect_after_backoff_and_replay(Ctx &c) {
  const char *name = "reconnect_after_backoff_and_replay";
  c.env.release_peer(c.peer);
  c.peer = -1;
  check(!loop_at(c, 50), name, "hub reconnected 50 ms after the drop, before the 100 ms retry delay");
  check(loop_at(c, 100), name, "hub did not reconnect 100 ms after the drop");
  check(c.child.connected == 2, name, "child saw " + std::to_string(c.child.connected) + " on_connected, want 2");
  c.peer = c.env.accept_peer();
  check(c.peer >= 0, name, "no second connection arrived at the listener");
  if (c.peer >= 0) check_replay(c, name);
}

// Drop 2: connection 2 got no welcome either, so the delay doubles: retry_delay_ 200, backoff_ 400.
void backoff_grows_without_welcome(Ctx &c) {
  const char *name = "backoff_grows_without_welcome";
  expect_drop(c, name, 2, 200, 400);
  c.env.release_peer(c.peer);
  c.peer = -1;
  check(!loop_at(c, 150), name, "hub reconnected 150 ms after the drop, before the 200 ms retry delay");
  check(loop_at(c, 200), name, "hub did not reconnect 200 ms after the drop");
  check(c.child.connected == 3, name, "child saw " + std::to_string(c.child.connected) + " on_connected, want 3");
  c.peer = c.env.accept_peer();
  check(c.peer >= 0, name, "no third connection arrived at the listener");
}

// Drop 3: connection 3 gets a welcome, which resets backoff_ to 100; its drop then retries after
// 100 ms and does not bump backoff_ (a welcomed session).
void welcome_resets_backoff(Ctx &c) {
  const char *name = "welcome_resets_backoff";
  if (c.peer >= 0) check_replay(c, name);
  const std::string welcome = "{\"k\":\"welcome\",\"proto\":1,\"t\":0}\n";
  check(::send(c.peer, welcome.data(), welcome.size(), MSG_NOSIGNAL) == static_cast<ssize_t>(welcome.size()), name,
        "could not write the welcome line");
  c.bus.loop();  // reads the welcome
  check(c.bus.connected(), name, "hub dropped the connection on a valid welcome");
  check(c.bus.backoff_ == 100, name, "backoff_ " + std::to_string(c.bus.backoff_) + " after the welcome, want 100");

  expect_drop(c, name, 3, 100, 100);
  c.env.release_peer(c.peer);
  c.peer = -1;
  check(!loop_at(c, 50), name, "hub reconnected 50 ms after the drop, before the 100 ms retry delay");
  check(loop_at(c, 100), name, "hub did not reconnect 100 ms after the drop");
  check(c.child.connected == 4, name, "child saw " + std::to_string(c.child.connected) + " on_connected, want 4");
  c.peer = c.env.accept_peer();
  check(c.peer >= 0, name, "no fourth connection arrived at the listener");
  if (c.peer >= 0) check_replay(c, name);
}

}  // namespace

int main() {
  // A blocking hub would hang here; SIGALRM turns that into a failed make (exit 3) and removes the
  // temp socket and directory first.
  std::signal(SIGALRM, on_watchdog);
  alarm(20);
  Ctx c;
  if (!c.env.init()) {
    std::fprintf(stderr, "cannot set up the test socket\n");
    return 2;
  }
  drop_above_cap(c);
  reconnect_after_backoff_and_replay(c);
  backoff_grows_without_welcome(c);
  welcome_resets_backoff(c);

  std::printf("txcap: %d passed, %d failed\n", g_passed, g_failed);
  return g_failed == 0 ? 0 : 1;
}
