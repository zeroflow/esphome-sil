#include "sim_bus.h"
#include "sim_protocol.h"
#include "esphome/core/application.h"
#include "esphome/core/hal.h"
#include "esphome/core/wake.h"
#include <algorithm>
#include <cerrno>
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

// The session follows protocol/PROTOCOL.md: on every (re)connect the hub sends hello, one chan per
// channel, the last value of every output and sync, and answers the simulator's welcome / in / sync.
// Every line goes through sim_protocol.h; any line it rejects closes the connection.
namespace esphome::sim_bus {
static const char *const TAG = "sim_bus";

static int64_t now_us() { return static_cast<int64_t>(millis_64()) * 1000; }

void SimBus::setup() {
  // SIL_BUS (set by the supervisor/tests) wins over the YAML path; an empty value is ignored.
  const char *env = std::getenv("SIL_BUS");
  if (env != nullptr && env[0] != '\0') this->path_ = env;
  this->try_connect_();
}
void SimBus::dump_config() {
  ESP_LOGCONFIG(TAG, "Sim bus: %s (%s)", this->path_.c_str(), this->fd_ >= 0 ? "connected" : "down");
}

// Reconnect delays: 100, 200, 400, 800, 1600, then 2000 ms (kBackoffMax) for every further attempt.
void SimBus::bump_backoff_() {
  this->retry_delay_ = this->backoff_;
  this->backoff_ = std::min<uint32_t>(this->backoff_ * 2, kBackoffMax);
}

void SimBus::try_connect_() {
  this->last_try_ = millis();
  int fd = ::socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
  if (fd < 0) {
    this->bump_backoff_();
    return;
  }
  sockaddr_un a{};
  a.sun_family = AF_UNIX;
  std::strncpy(a.sun_path, this->path_.c_str(), sizeof(a.sun_path) - 1);
  if (::connect(fd, reinterpret_cast<sockaddr *>(&a), sizeof(a)) != 0) {
    ::close(fd);  // ENOENT / ECONNREFUSED: simulator not up yet; retry from loop()
    this->bump_backoff_();
    return;
  }
  this->fd_ = fd;
  this->welcomed_ = this->synced_ = false;
  this->rx_.clear();
  this->tx_.clear();
  wake_register_fd(fd);  // select() in the main loop now wakes on inbound data
  ESP_LOGI(TAG, "connected to %s", this->path_.c_str());

  // Snapshot, in registration order: hello, the channel manifest, the last value of every
  // output (exactly once per connection), then sync.
  proto::Frame hello;
  hello.str("k", "hello")
      .i64("proto", proto::PROTO)
      .i64("chans", static_cast<int64_t>(this->children_.size()))
      .i64("t", now_us());
  hello.str("name", App.get_name().str());
  this->queue_(proto::encode(hello));
  for (auto *c : this->children_) {
    proto::Frame f;
    f.str("k", "chan").str("ch", c->channel()).str("dir", c->direction()).str("type", "f32");
    this->queue_(proto::encode(f));
  }
  for (const auto &e : this->last_out_) this->send_out_(e.first, e.second);
  proto::Frame sync;
  sync.str("k", "sync").i64("t", now_us());
  this->queue_(proto::encode(sync));
  for (auto *c : this->children_) c->on_connected();
  this->flush_();
}

void SimBus::close_() {
  const bool was_connected = this->fd_ >= 0;
  if (was_connected) {
    wake_unregister_fd(this->fd_);
    ::close(this->fd_);
  }
  this->fd_ = -1;
  this->rx_.clear();
  this->tx_.clear();
  this->last_try_ = millis();
  if (was_connected) {
    // A session that reached welcome retries quickly; one that did not backs off further.
    if (this->welcomed_) {
      this->retry_delay_ = this->backoff_;
    } else {
      this->bump_backoff_();
    }
    this->welcomed_ = this->synced_ = false;
    for (auto *c : this->children_) c->on_disconnected();
  }
}

void SimBus::flush_() {
  while (this->fd_ >= 0 && !this->tx_.empty()) {
    ssize_t n = ::send(this->fd_, this->tx_.data(), this->tx_.size(), MSG_NOSIGNAL | MSG_DONTWAIT);
    if (n > 0) {
      this->tx_.erase(0, n);
      continue;
    }
    if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) break;
    this->close_();
    return;
  }
  if (this->fd_ >= 0 && this->tx_.size() > kMaxTx) {
    // kMaxTx is 65536 bytes: more than that queued means the simulator stopped reading.
    ESP_LOGW(TAG, "sim bus: %u bytes pending for a simulator that does not read; dropping the connection",
             static_cast<unsigned>(this->tx_.size()));
    this->close_();
  }
}

void SimBus::loop() {
  if (this->fd_ < 0) {
    if (millis() - this->last_try_ >= this->retry_delay_) this->try_connect_();
    return;
  }
  char buf[1024];
  // Bounded number of reads per loop so a flooding peer cannot starve the main loop.
  for (int reads = 0; reads < 64; reads++) {
    ssize_t n = ::recv(this->fd_, buf, sizeof(buf), MSG_DONTWAIT);
    if (n > 0) {
      this->rx_.append(buf, n);
      size_t nl;
      while ((nl = this->rx_.find('\n')) != std::string::npos) {
        if (nl + 1 > proto::MAX_LINE) {  // a complete line that is too long is as broken as an unfinished one
          ESP_LOGW(TAG, "sim bus line longer than %u bytes; dropping the connection",
                   static_cast<unsigned>(proto::MAX_LINE));
          this->close_();
          return;
        }
        const std::string line = this->rx_.substr(0, nl);
        this->rx_.erase(0, nl + 1);
        if (!this->handle_line_(line)) return;
      }
      if (this->rx_.size() > proto::MAX_LINE) {
        ESP_LOGW(TAG, "sim bus line longer than %u bytes without newline; dropping the connection",
                 static_cast<unsigned>(proto::MAX_LINE));
        this->close_();
        return;
      }
      continue;
    }
    if (n == 0 || (errno != EAGAIN && errno != EWOULDBLOCK)) {
      ESP_LOGW(TAG, "sim bus closed");
      this->close_();
      return;
    }
    break;
  }
  this->flush_();
}

void SimBus::send_err_(const std::string &msg) {
  proto::Frame f;
  f.str("k", "err").str("msg", msg);
  this->queue_(proto::encode(f));
  this->flush_();  // best effort; the caller closes right after
}

bool SimBus::handle_line_(const std::string &line) {
  proto::Frame f;
  const proto::Err e = proto::parse(line, f);
  if (e != proto::Err::NONE) {
    ESP_LOGW(TAG, "bad line from the simulator (%s); dropping the connection", proto::err_name(e));
    this->send_err_(std::string("bad frame: ") + proto::err_name(e));
    this->close_();
    return false;
  }
  const std::string k = f.kind();
  if (k == "in") {
    // Accepted before the welcome too: a peer that only streams in frames still works.
    std::string ch;
    float v = 0;
    if (!f.get_str("ch", ch) || !f.get_num("v", v)) {
      ESP_LOGW(TAG, "in frame without ch or v ignored");
      return true;
    }
    for (auto *c : this->children_)
      if (c->channel() == ch && std::strcmp(c->direction(), "in") == 0) c->on_value(v);
  } else if (k == "welcome") {
    int64_t p = 0;
    if (!f.get_int("proto", p) || p != proto::PROTO) {
      ESP_LOGE(TAG, "simulator speaks protocol %lld, this firmware speaks %d; dropping the connection",
               static_cast<long long>(p), proto::PROTO);
      this->close_();
      return false;
    }
    this->welcomed_ = true;
    this->backoff_ = kBackoffMin;
  } else if (k == "sync") {
    this->synced_ = true;
    this->backoff_ = kBackoffMin;
    ESP_LOGI(TAG, "sim-bus synced");
  } else if (k == "err") {
    std::string msg;
    f.get_str("msg", msg);
    ESP_LOGW(TAG, "simulator reported an error: %s", msg.c_str());
    this->close_();
    return false;
  } else if (k == "bye") {
    ESP_LOGI(TAG, "simulator said bye; reconnecting");
    this->close_();
    return false;
  }
  return true;  // unknown kinds are ignored
}

void SimBus::send_out_(const std::string &ch, float v) {
  proto::Frame f;
  f.str("k", "out").str("ch", ch).num("v", v).i64("t", now_us());
  this->queue_(proto::encode(f));
}

void SimBus::set_out(const std::string &ch, float v) {
  if (!std::isfinite(v)) return;
  for (auto &e : this->last_out_) {
    if (e.first != ch) continue;
    if (e.second == v && this->fd_ >= 0) return;  // unchanged: nothing to send
    e.second = v;
    if (this->fd_ >= 0) {
      this->send_out_(ch, v);
      this->flush_();
    }
    return;
  }
  this->last_out_.emplace_back(ch, v);
  if (this->fd_ >= 0) {
    this->send_out_(ch, v);
    this->flush_();
  }
}

}  // namespace esphome::sim_bus
