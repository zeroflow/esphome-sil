#include "esphome/core/defines.h"
#ifdef USE_LIGHT
#include "sim_light.h"
#include "esphome/core/hal.h"
#include "esphome/core/log.h"
#include <algorithm>

namespace esphome::sim_bus {
static const char *const TAG = "sim_bus.light";

// 2 lowercase hex characters per byte, RGB order, exactly as the firmware wrote the buffer.
static std::string to_hex(const std::vector<uint8_t> &buf) {
  static const char *const digits = "0123456789abcdef";
  std::string s;
  s.reserve(buf.size() * 2);
  for (uint8_t b : buf) {
    s += digits[b >> 4];
    s += digits[b & 15];
  }
  return s;
}

void SimLight::setup() {
  this->buf_.assign(static_cast<size_t>(this->n_) * 3, 0);
  this->effect_.assign(this->n_, 0);
}

void SimLight::dump_config() {
  ESP_LOGCONFIG(TAG, "Sim bus light:");
  ESP_LOGCONFIG(TAG, "  Channel: %s", this->channel_.c_str());
  ESP_LOGCONFIG(TAG, "  LEDs: %u", static_cast<unsigned>(this->n_));
  ESP_LOGCONFIG(TAG, "  Segments: %s", this->segs_.c_str());
}

void SimLight::write_state(light::LightState *) {
  this->pending_ = true;
  this->maybe_send_();
}

void SimLight::loop() {
  if (this->pending_) this->maybe_send_();
}

void SimLight::maybe_send_() {
  if (this->bus_ == nullptr) return;
  const std::string cur = to_hex(this->buf_);
  if (this->sent_once_ && cur == this->last_sent_ && this->bus_->connected()) {
    this->pending_ = false;  // nothing changed since the peer last received this strip
    return;
  }
  const uint32_t now = millis();
  if (this->sent_once_ && now - this->last_send_ms_ < kMinGapMs) return;  // loop() retries
  this->last_send_ms_ = now;
  this->sent_once_ = true;
  this->last_sent_ = cur;
  this->pending_ = false;
  this->bus_->set_out_px(this->channel_, cur);  // stores for the replay, sends when connected
}

}  // namespace esphome::sim_bus
#endif  // USE_LIGHT
