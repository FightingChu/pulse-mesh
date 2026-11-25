#include "sensor.h"
#include <cstdio>
#include <random>

static const char* level_name(int level) {
  switch (level) {
    case 2: return "critical";
    case 1: return "warning";
    default: return "normal";
  }
}

int main() {
  double buf[8];
  char json_line[384];
  std::random_device entropy;
  char session_id[40];
  const int session_length = std::snprintf(
      session_id, sizeof(session_id), "sim-%08x%08x%08x%08x",
      static_cast<unsigned int>(entropy()), static_cast<unsigned int>(entropy()),
      static_cast<unsigned int>(entropy()), static_cast<unsigned int>(entropy()));
  if (session_length < 0 || static_cast<std::size_t>(session_length) >= sizeof(session_id)) return 1;
  char event_id[64];
  for (int tick = 0; tick < 50; tick++) {
    Reading r = sample_once(tick);
    for (int i = 0; i < 8; i++) buf[i] = r.vibration_rms;
    double rms = compute_rms(buf, 8);
    int level = classify(rms);
    const int event_length = std::snprintf(event_id, sizeof(event_id), "%s-sample-%d", session_id, tick);
    if (event_length < 0 || static_cast<std::size_t>(event_length) >= sizeof(event_id)) return 1;
    const std::size_t length = serialize_sample_json(
        json_line, sizeof(json_line), event_id, "motor-01", "vibration_rms", rms,
        level_name(level), r.temp_c);
    if (length == 0) return 1;
    std::puts(json_line);
  }

  constexpr std::size_t kN = 1024;
  constexpr double kSampleRate = 1000.0;
  constexpr double kRotation = 25.0;
  static double wave[kN];
  static SpectrumWorkspace workspace;
  synthesize_waveform(wave, kN, kSampleRate, kRotation, 0.6);
  Spectrum s = analyze_spectrum(wave, kN, kSampleRate, workspace);
  const int event_length = std::snprintf(event_id, sizeof(event_id), "%s-spectrum", session_id);
  if (event_length < 0 || static_cast<std::size_t>(event_length) >= sizeof(event_id)) return 1;
  const std::size_t spectrum_length = serialize_spectrum_json(
      json_line, sizeof(json_line), event_id, "motor-01", s,
      classify_fault(s, kRotation));
  if (spectrum_length == 0) return 1;
  std::puts(json_line);
  return 0;
}
