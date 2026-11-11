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

  constexpr std::size_t kN = 1024;
  constexpr double kSampleRate = 1000.0;
  constexpr double kRotation = 25.0;
  double wave[kN];
  synthesize_waveform(wave, kN, kSampleRate, kRotation, 0.6);
  Spectrum s = analyze_spectrum(wave, kN, kSampleRate);
  std::printf(
      "{\"device_id\":\"motor-01\",\"metric\":\"spectrum\",\"dominant_hz\":%.1f,"
      "\"low_band\":%.3f,\"mid_band\":%.3f,\"high_band\":%.3f,\"fault\":\"%s\"}\n",
      s.dominant_hz, s.low_band, s.mid_band, s.high_band,
      classify_fault(s, kRotation));
  return 0;
}
