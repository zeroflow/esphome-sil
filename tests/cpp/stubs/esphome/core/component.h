#pragma once
// Test stub of the ESPHome 2026.10.1 esphome/core/component.h: only what
// components/sim_bus/sim_bus.cpp uses, signatures mirroring the pinned image. Never used by a
// firmware build (ESPHome brings its own header). Like the real one it makes the fixed-width
// integer types available to its includers (sim_bus.h relies on that for uint32_t).
#include <cstdint>

namespace esphome {

namespace setup_priority {
inline constexpr float BUS = 1000.0f;
}  // namespace setup_priority

class Component {
 public:
  virtual ~Component() = default;
  virtual void setup() {}
  virtual void loop() {}
  virtual void dump_config() {}
  virtual float get_setup_priority() const { return 0.0f; }
};

}  // namespace esphome
