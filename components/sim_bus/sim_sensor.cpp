#include "esphome/core/defines.h"
#ifdef USE_SENSOR
#include "sim_sensor.h"
#include "esphome/core/hal.h"

namespace esphome::sim_bus {
static const char *const TAG = "sim_bus.sensor";

void SimSensor::dump_config() {
  LOG_SENSOR("", "Sim bus sensor", this);
  ESP_LOGCONFIG(TAG, "  Channel: %s", this->channel_.c_str());
  LOG_UPDATE_INTERVAL(this);
}

void SimValueSensor::dump_config() {
  LOG_SENSOR("", "Sim bus value sensor", this);
  ESP_LOGCONFIG(TAG, "  Channel: %s", this->channel_.c_str());
}

void SimSensor::on_value(float v) {
  const uint32_t now = millis();
  if (this->have_) {
    this->area_ += static_cast<double>(this->last_) * static_cast<uint32_t>(now - this->last_t_);
  } else {
    this->win_start_ = now;
    this->area_ = 0;
  }
  this->last_t_ = now;
  this->last_ = v;
  this->have_ = true;
}

void SimSensor::update() {
  // No value yet, or the simulator is gone: stop publishing so the last state stays (tach lost).
  if (!this->have_ || this->bus_ == nullptr || !this->bus_->connected()) return;
  const uint32_t now = millis();
  this->area_ += static_cast<double>(this->last_) * static_cast<uint32_t>(now - this->last_t_);
  const uint32_t window = now - this->win_start_;
  const float mean = window > 0 ? static_cast<float>(this->area_ / window) : this->last_;
  this->area_ = 0;
  this->win_start_ = this->last_t_ = now;
  this->publish_state(mean);
}

}  // namespace esphome::sim_bus
#endif  // USE_SENSOR
