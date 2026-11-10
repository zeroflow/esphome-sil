#pragma once
// Test stub of the ESPHome 2026.10.1 esphome/core/application.h: only App.get_name().str(), which
// sim_bus.cpp puts in the hello frame. Never used by a firmware build.
#include <string>

namespace esphome {

struct StubAppName {
  std::string value;
  std::string str() const { return value; }
};

struct StubApplication {
  StubAppName get_name() const { return StubAppName{"txcap"}; }
};

inline StubApplication App;

}  // namespace esphome
