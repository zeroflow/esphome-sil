#pragma once
// Hub of the sim-bus. This header is platform-free on purpose: it must compile in a config that
// has no output or no sensor. The platform classes live in sim_output.h and sim_sensor.h behind
// USE_OUTPUT / USE_SENSOR.
#include <string>
#include <utility>
#include <vector>
#include "esphome/core/component.h"
#include "esphome/core/log.h"

namespace esphome::sim_bus {

class SimBus;

/// Common base so the hub can route inbound values and tell children about the connection.
class SimChild {
 public:
  virtual ~SimChild() = default;
  void set_channel(const std::string &c) { this->channel_ = c; }
  void set_bus(SimBus *b) { this->bus_ = b; }
  const std::string &channel() const { return this->channel_; }
  /// "out" (firmware to simulator) or "in" (simulator to firmware).
  virtual const char *direction() const = 0;
  /// Wire type of the channel: "f32" (a number in `v`) or "rgb" (pixels in `px`).
  virtual const char *type() const { return "f32"; }
  /// LED count of an "rgb" channel; 0 for every other channel.
  virtual int pixels() const { return 0; }
  /// Segments of an "rgb" channel as `label:from:to,...` (empty for none); sent with the chan frame.
  virtual const std::string &segments() const {
    static const std::string none;
    return none;
  }
  virtual void on_value(float) {}
  virtual void on_connected() {}
  virtual void on_disconnected() {}

 protected:
  std::string channel_;
  SimBus *bus_{nullptr};
};

class SimBus : public Component {
 public:
  /// The connection is dropped (and re-established with backoff) when this much is queued for a
  /// simulator that does not read, so a stalled peer can never grow or block the firmware loop.
  static constexpr size_t kMaxTx = 65536;
  /// Reconnect delay: starts at kBackoffMin, doubles per attempt that does not reach a welcome,
  /// capped at kBackoffMax; a welcome resets it.
  static constexpr uint32_t kBackoffMin = 100;
  static constexpr uint32_t kBackoffMax = 2000;

  void set_path(const std::string &p) { this->path_ = p; }
  void register_child(SimChild *c) { this->children_.push_back(c); }
  void setup() override;
  void loop() override;
  void dump_config() override;
  float get_setup_priority() const override { return setup_priority::BUS; }

  bool connected() const { return this->fd_ >= 0; }
  const std::vector<SimChild *> &children() const { return this->children_; }
  /// Records the latest value of an output channel and sends it when connected and changed.
  /// Non-finite values are ignored: they have no wire form.
  void set_out(const std::string &ch, float v);
  /// Same for a pixel channel: `hex` is 2 lowercase hex characters per byte, RGB order. It is stored
  /// also while the bus is down, so the connect snapshot replays the strip state at reconnect (the
  /// newest strip the light handed over, changes made during the gap included).
  void set_out_px(const std::string &ch, const std::string &hex);

 protected:
  void try_connect_();
  void close_();
  /// Handles one received line; returns false when the connection was closed because of it.
  bool handle_line_(const std::string &line);
  void queue_(const std::string &line) { this->tx_ += line; }
  void send_out_(const std::string &ch, float v);
  void send_px_(const std::string &ch, const std::string &hex);
  void send_err_(const std::string &msg);
  void bump_backoff_();
  void flush_();
  std::string path_;
  int fd_{-1};
  uint32_t last_try_{0};
  uint32_t retry_delay_{0};
  uint32_t backoff_{kBackoffMin};
  bool welcomed_{false};
  bool synced_{false};
  std::string rx_, tx_;
  std::vector<SimChild *> children_;
  /// Latest value per output channel, in the order the channels were first written. While
  /// connected every entry equals what the peer last received; on connect all of it is replayed once.
  std::vector<std::pair<std::string, float>> last_out_;
  /// Same for pixel channels: replayed after last_out_ and before sync.
  std::vector<std::pair<std::string, std::string>> last_px_;
};

}  // namespace esphome::sim_bus
