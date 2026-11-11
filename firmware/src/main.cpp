#include "sensor.h"
#include <cstdio>

static const char* level_name(int level) {
  switch (level) {
    case 2: return "critical";
    case 1: return "warning";
    default: return "normal";
  }
}

int main() {
  double buf[8];
  for (int tick = 0; tick < 50; tick++) {
    Reading r = sample_once(tick);
    for (int i = 0; i < 8; i++) buf[i] = r.vibration_rms;
    double rms = compute_rms(buf, 8);
    int level = classify(rms);
    std::printf("{\"device_id\":\"motor-01\",\"metric\":\"vibration_rms\",\"value\":%.2f,\"level\":\"%s\",\"temp_c\":%.1f}\n", rms, level_name(level), r.temp_c);
  }
  return 0;
}
