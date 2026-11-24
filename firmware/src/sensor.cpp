#include "sensor.h"
#include <cmath>

namespace {
constexpr double kPi = 3.14159265358979323846;
bool is_power_of_two(std::size_t n) { return n >= 2 && (n & (n - 1)) == 0; }
}  // namespace

double compute_rms(const double* v, std::size_t n) {
  if (n == 0) return 0.0;
  double s = 0.0;
  for (std::size_t i = 0; i < n; i++) s += v[i] * v[i];
  return std::sqrt(s / (double)n);
}
int classify(double rms) {
  if (rms > 4.0) return 2;
  if (rms > 2.5) return 1;
  return 0;
}
Reading sample_once(int tick) {
  double base = 1.1 + 0.1 * (tick % 5);
  if (tick == 42) base = 4.8;
  return Reading{base, 55.0 + (tick % 10), 3.2};
}

void hann_window(float* x, std::size_t n) {
  if (n < 2) return;
  for (std::size_t i = 0; i < n; i++) {
    x[i] = (float)(x[i] * 0.5 * (1.0 - std::cos(2.0 * kPi * (double)i / (double)(n - 1))));
  }
}

void fft_radix2(float* re, float* im, std::size_t n) {
  if (!is_power_of_two(n)) return;

  for (std::size_t i = 1, j = 0; i < n; i++) {
    std::size_t bit = n >> 1;
    for (; (j & bit) != 0; bit >>= 1) j ^= bit;
    j ^= bit;
    if (i < j) {
      std::swap(re[i], re[j]);
      std::swap(im[i], im[j]);
    }
  }

  for (std::size_t len = 2; len <= n; len <<= 1) {
    const double angle = -2.0 * kPi / (double)len;
    const float wlen_re = (float)std::cos(angle);
    const float wlen_im = (float)std::sin(angle);
    for (std::size_t i = 0; i < n; i += len) {
      float w_re = 1.0f;
      float w_im = 0.0f;
      for (std::size_t k = 0; k < len / 2; k++) {
        const std::size_t a = i + k;
        const std::size_t b = a + len / 2;
        const float v_re = re[b] * w_re - im[b] * w_im;
        const float v_im = re[b] * w_im + im[b] * w_re;
        re[b] = re[a] - v_re;
        im[b] = im[a] - v_im;
        re[a] += v_re;
        im[a] += v_im;
        const float next_re = w_re * wlen_re - w_im * wlen_im;
        w_im = w_re * wlen_im + w_im * wlen_re;
        w_re = next_re;
      }
    }
  }
}

Spectrum analyze_spectrum(const double* samples, std::size_t n, double sample_rate_hz) {
  SpectrumWorkspace workspace;
  return analyze_spectrum(samples, n, sample_rate_hz, workspace);
}

Spectrum analyze_spectrum(const double* samples, std::size_t n, double sample_rate_hz,
                          SpectrumWorkspace& workspace) {
  Spectrum out{0.0, 0.0, 0.0, 0.0, 0.0};
  if (samples == nullptr || !is_power_of_two(n) || n > kMaxSpectrumSamples ||
      !std::isfinite(sample_rate_hz) || sample_rate_hz <= 0.0) {
    return out;
  }

  double mean = 0.0;
  for (std::size_t i = 0; i < n; i++) {
    if (!std::isfinite(samples[i])) return out;
    mean += samples[i];
  }
  mean /= (double)n;
  for (std::size_t i = 0; i < n; i++) {
    workspace.real[i] = (float)(samples[i] - mean);
    workspace.imaginary[i] = 0.0f;
  }

  hann_window(workspace.real.data(), n);
  fft_radix2(workspace.real.data(), workspace.imaginary.data(), n);

  const std::size_t half = n / 2;
  double total = 0.0;
  double peak_power = 0.0;
  std::size_t peak_bin = 1;
  for (std::size_t k = 1; k <= half; k++) {
    const double real = (double)workspace.real[k];
    const double imaginary = (double)workspace.imaginary[k];
    const double power = real * real + imaginary * imaginary;
    total += power;
    if (power > peak_power) {
      peak_power = power;
      peak_bin = k;
    }
  }
  if (total <= 0.0) return out;

  const double bin_hz = sample_rate_hz / (double)n;
  out.dominant_hz = (double)peak_bin * bin_hz;
  // Hann window has a coherent gain of 0.5, so compensate by 2x.
  out.peak_amplitude = std::sqrt(peak_power) * 4.0 / (double)n;

  double low = 0.0;
  double mid = 0.0;
  double high = 0.0;
  for (std::size_t k = 1; k <= half; k++) {
    const double hz = (double)k * bin_hz;
    const double real = (double)workspace.real[k];
    const double imaginary = (double)workspace.imaginary[k];
    const double power = real * real + imaginary * imaginary;
    if (hz < 10.0) {
      low += power;
    } else if (hz < 100.0) {
      mid += power;
    } else if (hz <= 500.0) {
      high += power;
    }
  }
  out.low_band = low / total;
  out.mid_band = mid / total;
  out.high_band = high / total;
  return out;
}

const char* classify_fault(const Spectrum& s, double rotation_hz) {
  if (s.high_band > 0.25) return "bearing_wear";
  if (rotation_hz > 0.0) {
    const double ratio = s.dominant_hz / rotation_hz;
    if (ratio >= 1.8 && ratio <= 2.2) return "misalignment";
    if (ratio >= 0.8 && ratio <= 1.2) return "imbalance";
  }
  if (s.low_band > 0.5) return "looseness";
  return "normal";
}

void synthesize_waveform(double* out, std::size_t n, double sample_rate_hz,
                         double rotation_hz, double bearing_amplitude) {
  for (std::size_t i = 0; i < n; i++) {
    const double t = (double)i / sample_rate_hz;
    const double fundamental = std::sin(2.0 * kPi * rotation_hz * t);
    const double harmonic = 0.15 * std::sin(2.0 * kPi * 2.0 * rotation_hz * t);
    const double bearing =
        bearing_amplitude * std::sin(2.0 * kPi * 120.0 * t);
    out[i] = fundamental + harmonic + bearing;
  }
}
