#include "sim_bus.h"
#include "esphome/core/hal.h"
#include "esphome/core/wake.h"
#include <cerrno>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

namespace esphome::sim_bus {
static const char *const TAG = "sim_bus";

// Flat-object NDJSON helpers: no nesting, string/number scalars only.
static bool get_str(const std::string &l, const char *key, std::string &out) {
  std::string pat = std::string("\"") + key + "\":\"";
  auto p = l.find(pat);
  if (p == std::string::npos) return false;
  p += pat.size();
  auto e = l.find('"', p);
  if (e == std::string::npos) return false;
  out = l.substr(p, e - p);
  return true;
}
static bool get_num(const std::string &l, const char *key, float &out) {
  std::string pat = std::string("\"") + key + "\":";
  auto p = l.find(pat);
  if (p == std::string::npos) return false;
  out = std::strtof(l.c_str() + p + pat.size(), nullptr);
  return true;
}

void SimBus::setup() {
  // SIL_BUS (set by the supervisor/tests) wins over the YAML path; an empty value is ignored.
  const char *env = std::getenv("SIL_BUS");
  if (env != nullptr && env[0] != '\0') this->path_ = env;
  this->try_connect_();
}
void SimBus::dump_config() {
  ESP_LOGCONFIG(TAG, "Sim bus: %s (%s)", this->path_.c_str(), this->fd_ >= 0 ? "connected" : "down");
}

void SimBus::try_connect_() {
  this->last_try_ = millis();
  int fd = ::socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
  if (fd < 0) return;
  sockaddr_un a{};
  a.sun_family = AF_UNIX;
  std::strncpy(a.sun_path, this->path_.c_str(), sizeof(a.sun_path) - 1);
  if (::connect(fd, reinterpret_cast<sockaddr *>(&a), sizeof(a)) != 0) {
    ::close(fd);  // ENOENT / ECONNREFUSED: simulator not up yet; retry from loop()
    return;
  }
  this->fd_ = fd;
  wake_register_fd(fd);  // select() in the main loop now wakes on inbound data
  ESP_LOGI(TAG, "connected to %s", this->path_.c_str());
  this->tx_ += "{\"k\":\"hello\",\"proto\":1}\n";
  // Snapshot: the last value of every output, exactly once per connection.
  for (const auto &e : this->last_out_) this->send_out_(e.first, e.second);
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
  if (was_connected)
    for (auto *c : this->children_) c->on_disconnected();
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
}

void SimBus::loop() {
  if (this->fd_ < 0) {
    if (millis() - this->last_try_ >= 500) this->try_connect_();
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
        if (nl + 1 > kMaxLine) {  // a complete line that is too long is as broken as an unfinished one
          ESP_LOGW(TAG, "sim bus line longer than %u bytes; dropping the connection", static_cast<unsigned>(kMaxLine));
          this->close_();
          return;
        }
        this->handle_line_(this->rx_.substr(0, nl));
        this->rx_.erase(0, nl + 1);
      }
      if (this->rx_.size() > kMaxLine) {
        ESP_LOGW(TAG, "sim bus line longer than %u bytes without newline; dropping the connection",
                 static_cast<unsigned>(kMaxLine));
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

void SimBus::handle_line_(const std::string &line) {
  std::string k, ch;
  float v = 0;
  if (!get_str(line, "k", k) || k != "in" || !get_str(line, "ch", ch) || !get_num(line, "v", v)) return;
  for (auto *c : this->children_)
    if (c->channel() == ch) c->on_value(v);
}

void SimBus::send_out_(const std::string &ch, float v) {
  char b[160];
  std::snprintf(b, sizeof(b), "{\"k\":\"out\",\"ch\":\"%s\",\"v\":%.6g}\n", ch.c_str(), v);
  this->tx_ += b;
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
