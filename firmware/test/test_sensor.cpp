#include "sensor.h"

#include <cmath>
#include <cstdio>
#include <cstring>
#include <vector>

namespace {
int failures = 0;

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
  const double constant[] = {3.0, 3.0, 3.0, 3.0};
  expect_near("rms constant", compute_rms(constant, 4), 3.0, 1e-9);
  expect_near("rms empty", compute_rms(nullptr, 0), 0.0, 1e-9);

  std::vector<double> sine(1024);
  for (std::size_t i = 0; i < sine.size(); i++) {
    sine[i] = std::sin(2.0 * 3.14159265358979323846 * 25.0 * (double)i / 1000.0);
  }
  Spectrum s = analyze_spectrum(sine.data(), sine.size(), 1000.0);
  expect_near("dominant 25hz", s.dominant_hz, 25.0, 2.0);
  expect_near("dominant amp", s.peak_amplitude, 1.0, 0.1);
  expect_str("imbalance", classify_fault(s, 25.0), "imbalance");

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

  if (failures != 0) {
    std::printf("%d checks failed\n", failures);
    return 1;
  }
  std::printf("all checks passed\n");
  return 0;
}
