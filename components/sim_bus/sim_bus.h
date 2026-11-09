#pragma once
#include <string>
#include <vector>
#include "esphome/core/component.h"
#include "esphome/core/log.h"
#include "esphome/components/output/float_output.h"
#include "esphome/components/sensor/sensor.h"

namespace esphome::sim_bus {

class SimBus;

/// Common base so the hub can route inbound values and flush outbound state.
class SimChild {
 public:
  void set_channel(const std::string &c) { this->channel_ = c; }
  void set_bus(SimBus *b) { this->bus_ = b; }
  const std::string &channel() const { return this->channel_; }
  virtual void on_value(float v) {}
  virtual void on_connected() {}

 protected:
  std::string channel_;
  SimBus *bus_{nullptr};
};

class SimBus : public Component {
 public:
  void set_path(const std::string &p) { this->path_ = p; }
  void register_child(SimChild *c) { this->children_.push_back(c); }
  void setup() override;
  void loop() override;
  void dump_config() override;
  float get_setup_priority() const override { return setup_priority::BUS; }
  void send_value(const std::string &ch, float v);

 protected:
  void try_connect_();
  void close_();
  void handle_line_(const std::string &line);
  void flush_();
  std::string path_;
  int fd_{-1};
  uint32_t last_try_{0};
  std::string rx_, tx_;
  std::vector<SimChild *> children_;
};

class SimOutput : public output::FloatOutput, public Component, public SimChild {
 public:
  void write_state(float state) override;
  void on_connected() override { this->write_state(this->last_); }

 protected:
  float last_{0};
};

class SimSensor : public sensor::Sensor, public Component, public SimChild {
 public:
  void on_value(float v) override { this->publish_state(v); }
};

}  // namespace esphome::sim_bus
