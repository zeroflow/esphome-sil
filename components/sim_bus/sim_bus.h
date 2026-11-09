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
  virtual void on_value(float v) {}
  virtual void on_connected() {}
  virtual void on_disconnected() {}

 protected:
  std::string channel_;
  SimBus *bus_{nullptr};
};

class SimBus : public Component {
 public:
  /// A line longer than this without a newline drops the connection (the peer is broken or hostile).
  static constexpr size_t kMaxLine = 4096;

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

 protected:
  void try_connect_();
  void close_();
  void handle_line_(const std::string &line);
  void send_out_(const std::string &ch, float v);
  void flush_();
  std::string path_;
  int fd_{-1};
  uint32_t last_try_{0};
  std::string rx_, tx_;
  std::vector<SimChild *> children_;
  /// Latest value per output channel, in the order the channels were first written. While
  /// connected every entry equals what the peer last received; on connect all of it is replayed once.
  std::vector<std::pair<std::string, float>> last_out_;
};

}  // namespace esphome::sim_bus
