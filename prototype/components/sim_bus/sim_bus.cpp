#include "sim_bus.h"
#include "esphome/core/hal.h"
#include "esphome/core/wake.h"
#include <cerrno>
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

void SimBus::setup() { this->try_connect_(); }
void SimBus::dump_config() { ESP_LOGCONFIG(TAG, "Sim bus: %s (%s)", this->path_.c_str(), this->fd_ >= 0 ? "connected" : "down"); }

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
  for (auto *c : this->children_) c->on_connected();
  this->flush_();
}

void SimBus::close_() {
  if (this->fd_ >= 0) { wake_unregister_fd(this->fd_); ::close(this->fd_); }
  this->fd_ = -1; this->rx_.clear(); this->tx_.clear();
}

void SimBus::flush_() {
  while (this->fd_ >= 0 && !this->tx_.empty()) {
    ssize_t n = ::send(this->fd_, this->tx_.data(), this->tx_.size(), MSG_NOSIGNAL | MSG_DONTWAIT);
    if (n > 0) { this->tx_.erase(0, n); continue; }
    if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) break;
    this->close_(); return;
  }
}

void SimBus::loop() {
  if (this->fd_ < 0) {
    if (millis() - this->last_try_ >= 500) this->try_connect_();
    return;
  }
  char buf[1024];
  for (;;) {
    ssize_t n = ::recv(this->fd_, buf, sizeof(buf), MSG_DONTWAIT);
    if (n > 0) { this->rx_.append(buf, n); continue; }
    if (n == 0 || (errno != EAGAIN && errno != EWOULDBLOCK)) { ESP_LOGW(TAG, "sim bus closed"); this->close_(); return; }
    break;
  }
  size_t nl;
  while ((nl = this->rx_.find('\n')) != std::string::npos) {
    this->handle_line_(this->rx_.substr(0, nl));
    this->rx_.erase(0, nl + 1);
  }
  this->flush_();
}

void SimBus::handle_line_(const std::string &line) {
  std::string k, ch; float v = 0;
  if (!get_str(line, "k", k) || k != "in" || !get_str(line, "ch", ch) || !get_num(line, "v", v)) return;
  for (auto *c : this->children_) if (c->channel() == ch) c->on_value(v);
}

void SimBus::send_value(const std::string &ch, float v) {
  char b[160]; std::snprintf(b, sizeof(b), "{\"k\":\"out\",\"ch\":\"%s\",\"v\":%.6g}\n", ch.c_str(), v);
  if (this->fd_ >= 0) { this->tx_ += b; this->flush_(); }
}
void SimBus::send_text(const std::string &ch, const char *key, const std::string &s) {
  if (this->fd_ < 0) return;
  this->tx_ += "{\"k\":\"out\",\"ch\":\"" + ch + "\",\"" + key + "\":\"" + s + "\"}\n";
  this->flush_();
}

void SimOutput::write_state(float state) { this->last_ = state; this->bus_->send_value(this->channel_, state); }

void SimLight::setup() { this->buf_.assign(this->n_ * 3, 0); this->effect_.assign(this->n_, 0); }
void SimLight::write_state(light::LightState *state) {
  static const char *hex = "0123456789abcdef";
  std::string s; s.reserve(this->buf_.size() * 2);
  for (uint8_t b : this->buf_) { s += hex[b >> 4]; s += hex[b & 15]; }
  this->bus_->send_text(this->channel_, "rgb", s);
}
}  // namespace esphome::sim_bus
