#include "sensor.h"

#include <cmath>
#include <cstdio>
#include <cstring>
#include <limits>
#include <vector>

static_assert(sizeof(SpectrumWorkspace) ==
              (kMaxSpectrumSamples * 2 + kFftTwiddleChunk * 2) * sizeof(float));

namespace {
int failures = 0;
constexpr double kPiTest = 3.14159265358979323846;

void expect_near(const char* name, double got, double want, double tol) {
  if (std::fabs(got - want) > tol) {
    std::printf("FAIL %s: got %.6f want %.6f\n", name, got, want);
    failures++;
  } else {
    std::printf("ok   %s\n", name);
  }
}

void expect_str(const char* name, const char* got, const char* want) {
  if (std::strcmp(got, want) != 0) {
    std::printf("FAIL %s: got %s want %s\n", name, got, want);
    failures++;
  } else {
    std::printf("ok   %s\n", name);
  }
}
}  // namespace

int main() {
  char json_line[256];
  const std::size_t sample_json_size = serialize_sample_json(
      json_line, sizeof(json_line), "event\"42", "motor\\01", "vibration_rms", 4.8,
      "critical", 56.5);
  expect_near("sample JSON length", (double)sample_json_size, (double)std::strlen(json_line), 0.0);
  expect_str("sample JSON escaping", json_line,
             "{\"event_id\":\"event\\\"42\",\"device_id\":\"motor\\\\01\",\"metric\":\"vibration_rms\",\"value\":4.80,\"level\":\"critical\",\"temp_c\":56.5}");
  char tiny_json[8];
  expect_near("sample JSON small buffer",
              (double)serialize_sample_json(tiny_json, sizeof(tiny_json), "event", "motor-01",
                                            "vibration_rms", 1.0, "normal", 55.0),
              0.0, 0.0);
  expect_near("sample JSON non-finite value",
              (double)serialize_sample_json(json_line, sizeof(json_line), "event", "motor-01",
                                            "vibration_rms", NAN, "normal", 55.0),
              0.0, 0.0);
  const Spectrum serialized_spectrum{120.1, 0.6, 0.0, 0.1, 0.9};
  const std::size_t spectrum_json_size = serialize_spectrum_json(
      json_line, sizeof(json_line), "event-spectrum", "motor-01", serialized_spectrum,
      "bearing_wear");
  expect_near("spectrum JSON length", (double)spectrum_json_size, (double)std::strlen(json_line), 0.0);
  expect_str("spectrum JSON fields", json_line,
             "{\"event_id\":\"event-spectrum\",\"device_id\":\"motor-01\",\"metric\":\"spectrum\",\"dominant_hz\":120.1,\"peak_amplitude\":0.600,\"low_band\":0.000,\"mid_band\":0.100,\"high_band\":0.900,\"fault\":\"bearing_wear\"}");

  const double reference_input[] = {1.0, 0.0, -1.0, 0.0, 2.0, 0.0, -2.0, 0.0};
  float fft_real[8];
  for (std::size_t i = 0; i < 8; i++) fft_real[i] = (float)reference_input[i];
  float fft_imaginary[8]{};
  fft_radix2(fft_real, fft_imaginary, 8);
  for (std::size_t k = 0; k < 8; k++) {
    double expected_real = 0.0;
    double expected_imaginary = 0.0;
    for (std::size_t sample = 0; sample < 8; sample++) {
      const double phase = -2.0 * kPiTest * (double)(k * sample) / 8.0;
      expected_real += reference_input[sample] * std::cos(phase);
      expected_imaginary += reference_input[sample] * std::sin(phase);
    }
    expect_near("radix2 real vs DFT", fft_real[k], expected_real, 1e-5);
    expect_near("radix2 imaginary vs DFT", fft_imaginary[k], expected_imaginary, 1e-5);
  }

  float window[9];
  for (float& sample : window) sample = 1.0f;
  hann_window(window, 9);
  expect_near("hann left endpoint", window[0], 0.0, 1e-7);
  expect_near("hann right endpoint", window[8], 0.0, 1e-7);
  for (std::size_t i = 1; i < 8; i++) {
    expect_near("hann symmetry", window[i], window[8 - i], 1e-6);
  }

  const double constant[] = {3.0, 3.0, 3.0, 3.0};
  expect_near("rms constant", compute_rms(constant, 4), 3.0, 1e-9);
  expect_near("rms empty", compute_rms(nullptr, 0), 0.0, 1e-9);
  for (double magnitude : {1e308, std::numeric_limits<double>::max(), 1e-300,
                           std::numeric_limits<double>::denorm_min()}) {
    const double extreme[] = {magnitude, -magnitude, magnitude, -magnitude};
    const double actual = compute_rms(extreme, 4);
    if (!std::isfinite(actual) || actual <= 0.0) {
      std::printf("FAIL rms finite nonzero range: input %.17g result %.17g\n", magnitude, actual);
      failures++;
    } else {
      expect_near("rms scaled constant", actual / magnitude, 1.0, 1e-12);
    }
  }
  const double mixed[] = {0.0, 1e308, 0.0, -1e308};
  expect_near("rms mixed large values", compute_rms(mixed, 4) / 1e308,
              std::sqrt(0.5), 1e-12);
  const double zeros[] = {0.0, -0.0, 0.0, -0.0};
  expect_near("rms zeros", compute_rms(zeros, 4), 0.0, 0.0);
  for (double invalid_value : {NAN, INFINITY, -INFINITY}) {
    const double invalid_rms_input[] = {1.0, invalid_value};
    if (std::isfinite(compute_rms(invalid_rms_input, 2))) {
      std::printf("FAIL rms invalid input became finite\n");
      failures++;
    }
  }

  std::vector<double> sine(1024);
  for (std::size_t i = 0; i < sine.size(); i++) {
    sine[i] = std::sin(2.0 * 3.14159265358979323846 * 25.0 * (double)i / 1000.0);
  }
  Spectrum s = analyze_spectrum(sine.data(), sine.size(), 1000.0);
  expect_near("dominant 25hz", s.dominant_hz, 25.0, 2.0);
  expect_near("dominant amp", s.peak_amplitude, 1.0, 0.1);
  expect_str("imbalance", classify_fault(s, 25.0), "imbalance");

  SpectrumWorkspace workspace{};
  Spectrum reused = analyze_spectrum(sine.data(), sine.size(), 1000.0, workspace);
  expect_near("workspace reuse dominant", reused.dominant_hz, 25.0, 2.0);
  expect_near("workspace reuse amplitude", reused.peak_amplitude, 1.0, 0.1);

  std::vector<double> biased_sine(1024);
  for (std::size_t i = 0; i < biased_sine.size(); i++) {
    biased_sine[i] = 1000000000000.0 +
                     std::sin(2.0 * 3.14159265358979323846 * 25.0 * (double)i / 1000.0);
  }
  Spectrum biased = analyze_spectrum(biased_sine.data(), biased_sine.size(), 1000.0, workspace);
  expect_near("large DC offset dominant", biased.dominant_hz, 25.0, 2.0);
  expect_near("large DC offset amplitude", biased.peak_amplitude, 1.0, 0.1);

  std::vector<double> doubled(1024);
  for (std::size_t i = 0; i < doubled.size(); i++) {
    doubled[i] = std::sin(2.0 * 3.14159265358979323846 * 50.0 * (double)i / 1000.0);
  }
  Spectrum s2 = analyze_spectrum(doubled.data(), doubled.size(), 1000.0);
  expect_near("dominant 50hz", s2.dominant_hz, 50.0, 2.0);
  expect_str("misalignment", classify_fault(s2, 25.0), "misalignment");

  std::vector<double> bearing(1024);
  for (std::size_t i = 0; i < bearing.size(); i++) {
    bearing[i] = std::sin(2.0 * 3.14159265358979323846 * 120.0 * (double)i / 1000.0);
  }
  Spectrum s3 = analyze_spectrum(bearing.data(), bearing.size(), 1000.0);
  expect_near("dominant 120hz", s3.dominant_hz, 120.0, 2.0);
  expect_str("bearing wear", classify_fault(s3, 25.0), "bearing_wear");

  Spectrum empty{};
  expect_str("flat spectrum normal", classify_fault(empty, 25.0), "normal");

  std::vector<double> too_large(kMaxSpectrumSamples * 2, 1.0);
  Spectrum rejected = analyze_spectrum(too_large.data(), too_large.size(), 1000.0, workspace);
  expect_near("oversized spectrum rejected", rejected.peak_amplitude, 0.0, 1e-9);

  double invalid[] = {0.0, 1.0, NAN, 2.0};
  Spectrum invalid_result = analyze_spectrum(invalid, 4, 1000.0, workspace);
  expect_near("non-finite spectrum rejected", invalid_result.peak_amplitude, 0.0, 1e-9);

  double out_of_range[] = {0.0, std::numeric_limits<double>::max(), 0.0, 0.0};
  Spectrum range_result = analyze_spectrum(out_of_range, 4, 1000.0, workspace);
  expect_near("float workspace overflow rejected", range_result.peak_amplitude, 0.0, 1e-9);

  if (failures != 0) {
    std::printf("%d checks failed\n", failures);
    return 1;
  }
  std::printf("all checks passed\n");
  return 0;
}
