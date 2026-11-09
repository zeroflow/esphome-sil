#pragma once
#include "esphome/core/defines.h"
#ifdef USE_OUTPUT
#include "esphome/components/output/float_output.h"
#include "sim_bus.h"

namespace esphome::sim_bus {

/// PWM duty of one fan port, sent to the simulator as an `out` frame on every change.
class SimOutput : public output::FloatOutput, public Component, public SimChild {
 public:
  const char *direction() const override { return "out"; }
  void write_state(float state) override;
};

}  // namespace esphome::sim_bus
#endif  // USE_OUTPUT
