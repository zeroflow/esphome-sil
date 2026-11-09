#pragma once
#include "esphome/core/defines.h"
#ifdef USE_SENSOR
#include "esphome/components/sensor/sensor.h"
#include "sim_bus.h"

namespace esphome::sim_bus {

/// RPM of one fan port. The simulator pushes values; update() (default every 1 s) publishes the
/// time-weighted mean of the values received since the last publish, like a tach counted per second.
class SimSensor : public sensor::Sensor, public PollingComponent, public SimChild {
 public:
  const char *direction() const override { return "in"; }
  void dump_config() override;
  void on_value(float v) override;
  void on_connected() override { this->reset_window_(); }
  void on_disconnected() override { this->reset_window_(); }
  void update() override;

 protected:
  void reset_window_() {
    this->have_ = false;
    this->area_ = 0;
  }
  bool have_{false};
  float last_{0};
  uint32_t last_t_{0};     // ms of the last value
  uint32_t win_start_{0};  // ms the current window started
  double area_{0};         // value * ms accumulated in the window up to last_t_
};

}  // namespace esphome::sim_bus
#endif  // USE_SENSOR
