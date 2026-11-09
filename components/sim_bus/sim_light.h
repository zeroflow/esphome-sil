#pragma once
#include "esphome/core/defines.h"
#ifdef USE_LIGHT
#include <string>
#include <vector>
#include "esphome/components/light/addressable_light.h"
#include "sim_bus.h"

namespace esphome::sim_bus {

/// The status LED strip. ESPHome (a plain colour, an effect such as `pulse`, or the stock
/// `partition` lights that carve the strip into segments) writes the final pixel bytes into the
/// buffer, in RGB order, with brightness, colour correction and effects already applied. The light
/// mirrors the buffer to the simulator as the `px` hex string of an `out` frame.
///
/// Two rules keep the bus quiet (D-07). A frame is sent only when its content differs from the
/// last one sent, and never within kMinGapMs of the previous one by the firmware's own clock, so
/// the rate is capped at 20 Hz however often ESPHome rewrites the strip (a `pulse` transition
/// writes it on every main-loop pass). A change that has to wait stays pending and is sent from
/// loop(), so the final colour of a burst always leaves.
class SimLight : public light::AddressableLight, public SimChild {
 public:
  /// Minimum distance between two frames, in firmware milliseconds (consecutive `t` differ by >= 50000 us).
  static constexpr uint32_t kMinGapMs = 50;

  const char *direction() const override { return "out"; }
  const char *type() const override { return "rgb"; }
  int pixels() const override { return this->n_; }
  const std::string &segments() const override { return this->segs_; }
  void set_segments(const std::string &s) { this->segs_ = s; }
  void set_num_leds(uint16_t n) { this->n_ = n; }

  void setup() override;
  void loop() override;
  void dump_config() override;
  float get_setup_priority() const override { return setup_priority::HARDWARE; }
  int32_t size() const override { return this->n_; }
  void clear_effect_data() override { std::fill(this->effect_.begin(), this->effect_.end(), 0); }
  light::LightTraits get_traits() override {
    auto t = light::LightTraits();
    t.set_supported_color_modes({light::ColorMode::RGB});
    return t;
  }
  void write_state(light::LightState *state) override;
  /// The hub has just replayed the strip state it holds; whatever changed since is still pending.
  void on_connected() override { this->pending_ = true; }

 protected:
  light::ESPColorView get_view_internal(int32_t i) const override {
    uint8_t *p = const_cast<uint8_t *>(this->buf_.data()) + i * 3;
    return {p + 0, p + 1, p + 2, nullptr, const_cast<uint8_t *>(&this->effect_[i]), &this->correction_};
  }
  void maybe_send_();

  uint16_t n_{0};
  std::string segs_, last_sent_;
  std::vector<uint8_t> buf_, effect_;
  bool pending_{false};
  bool sent_once_{false};
  uint32_t last_send_ms_{0};
};

}  // namespace esphome::sim_bus
#endif  // USE_LIGHT
