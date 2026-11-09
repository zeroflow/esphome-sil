#pragma once
#include "esphome/core/defines.h"
#ifdef USE_BINARY_SENSOR
#include "esphome/components/binary_sensor/binary_sensor.h"
#include "sim_bus.h"

namespace esphome::sim_bus {

/// A button (USR1-3) as a level: the simulator sends 0 or 1 on an `in` channel and the firmware
/// measures the hold time itself (on_click windows), exactly as on the hardware. The simulator
/// must send the released level first (its in-snapshot does): ESPHome does not run state
/// callbacks for the first state of an entity. A lost simulator keeps the last level; the
/// release arrives with the next in-snapshot.
class SimBinarySensor : public binary_sensor::BinarySensor, public Component, public SimChild {
 public:
  const char *direction() const override { return "in"; }
  void dump_config() override {
    static const char *const TAG = "sim_bus.binary_sensor";  // local: the logging macros name TAG
    LOG_BINARY_SENSOR("", "Sim bus binary sensor", this);
    ESP_LOGCONFIG(TAG, "  Channel: %s", this->channel_.c_str());
  }
  void on_value(float v) override { this->publish_state(v > 0.5f); }
};

}  // namespace esphome::sim_bus
#endif  // USE_BINARY_SENSOR
