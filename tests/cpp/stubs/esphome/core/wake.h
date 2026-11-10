#pragma once
// Test stub of the ESPHome 2026.10.1 esphome/core/wake.h: the host main-loop fd registration, a
// no-op here. Never used by a firmware build.
namespace esphome {

inline bool wake_register_fd(int) { return true; }
inline void wake_unregister_fd(int) {}

}  // namespace esphome
