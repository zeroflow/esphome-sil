#include "esphome/core/defines.h"
#ifdef USE_OUTPUT
#include "sim_output.h"

namespace esphome::sim_bus {

void SimOutput::write_state(float state) {
  if (this->bus_ != nullptr) this->bus_->set_out(this->channel_, state);
}

}  // namespace esphome::sim_bus
#endif  // USE_OUTPUT
