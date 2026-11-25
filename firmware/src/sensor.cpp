#include "sensor.h"
#include <cmath>
#include <cstdio>
#include <limits>

namespace {
constexpr double kPi = 3.14159265358979323846;
constexpr std::size_t kHannReanchorInterval = 32;
bool is_power_of_two(std::size_t n) { return n >= 2 && (n & (n - 1)) == 0; }

class JsonWriter {
 public:
  JsonWriter(char* output, std::size_t capacity) : output_(output), capacity_(capacity) {
    if (capacity_ > 0 && output_ != nullptr) output_[0] = '\0';
  }

  bool append(const char* text) {
    if (text == nullptr) return false;
    while (*text != '\0') {
      if (!appendChar(*text++)) return false;
    }
    return true;
  }

  bool appendDouble(double value, int precision) {
    if (!std::isfinite(value)) return false;
    char number[48];
    const int length = std::snprintf(number, sizeof(number), "%.*f", precision, value);
    if (length < 0 || (std::size_t)length >= sizeof(number)) return false;
    return append(number);
  }

  bool appendString(const char* value) {
    if (value == nullptr || !appendChar('"')) return false;
    for (const unsigned char* cursor = (const unsigned char*)value; *cursor != 0; cursor++) {
      const char* escaped = nullptr;
      switch (*cursor) {
        case '"': escaped = "\\\""; break;
        case '\\': escaped = "\\\\"; break;
        case '\b': escaped = "\\b"; break;
        case '\f': escaped = "\\f"; break;
        case '\n': escaped = "\\n"; break;
        case '\r': escaped = "\\r"; break;
        case '\t': escaped = "\\t"; break;
        default: break;
      }
      if (escaped != nullptr) {
        if (!append(escaped)) return false;
      } else if (*cursor < 0x20) {
        char control[7];
        const int length = std::snprintf(control, sizeof(control), "\\u%04x", *cursor);
        if (length != 6 || !append(control)) return false;
      } else if (!appendChar((char)*cursor)) {
        return false;
      }
    }
    return appendChar('"');
  }

  std::size_t size() const { return length_; }

 private:
  bool appendChar(char value) {
    if (output_ == nullptr || length_ + 1 >= capacity_) return false;
    output_[length_++] = value;
    output_[length_] = '\0';
    return true;
  }

  char* output_;
  std::size_t capacity_;
  std::size_t length_ = 0;
};

void fft_radix2_impl(float* re, float* im, std::size_t n, float* twiddle_real,
                     float* twiddle_imaginary) {
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
    const std::size_t half = len / 2;
    const double angle_step = -2.0 * kPi / (double)len;
    const double step_real = std::cos(angle_step);
    const double step_imaginary = std::sin(angle_step);
    for (std::size_t chunk_start = 0; chunk_start < half;
         chunk_start += kFftTwiddleChunk) {
      const std::size_t chunk_length =
          (half - chunk_start < kFftTwiddleChunk) ? half - chunk_start : kFftTwiddleChunk;
      const double start_angle = angle_step * (double)chunk_start;
      double twiddle_real_value = std::cos(start_angle);
      double twiddle_imaginary_value = std::sin(start_angle);
      for (std::size_t offset = 0; offset < chunk_length; offset++) {
        twiddle_real[offset] = (float)twiddle_real_value;
        twiddle_imaginary[offset] = (float)twiddle_imaginary_value;
        const double next_real = twiddle_real_value * step_real -
                                 twiddle_imaginary_value * step_imaginary;
        twiddle_imaginary_value = twiddle_real_value * step_imaginary +
                                  twiddle_imaginary_value * step_real;
        twiddle_real_value = next_real;
      }

      for (std::size_t group = 0; group < n; group += len) {
        for (std::size_t offset = 0; offset < chunk_length; offset++) {
          const std::size_t k = chunk_start + offset;
          const std::size_t a = group + k;
          const std::size_t b = a + half;
          const float w_real = twiddle_real[offset];
          const float w_imaginary = twiddle_imaginary[offset];
          const float value_real = re[b] * w_real - im[b] * w_imaginary;
          const float value_imaginary = re[b] * w_imaginary + im[b] * w_real;
          re[b] = re[a] - value_real;
          im[b] = im[a] - value_imaginary;
          re[a] += value_real;
          im[a] += value_imaginary;
        }
      }
    }
  }
}
}  // namespace

double compute_rms(const double* v, std::size_t n) {
  if (n == 0) return 0.0;
  double scale = 0.0;
  for (std::size_t index = 0; index < n; index++) {
    if (!std::isfinite(v[index])) return std::numeric_limits<double>::quiet_NaN();
    const double magnitude = std::fabs(v[index]);
    if (magnitude > scale) scale = magnitude;
  }
  if (scale == 0.0) return 0.0;
  double sum = 0.0;
  for (std::size_t index = 0; index < n; index++) {
    const double normalized = v[index] / scale;
    sum += normalized * normalized;
  }
  return scale * std::sqrt(sum / (double)n);
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
  const double phase_step = 2.0 * kPi / (double)(n - 1);
  const double step_cos = std::cos(phase_step);
  const double step_sin = std::sin(phase_step);
  double phase_cos = 1.0;
  double phase_sin = 0.0;
  for (std::size_t i = 0; i < n; i++) {
    if (i % kHannReanchorInterval == 0) {
      const double phase = phase_step * (double)i;
      phase_cos = std::cos(phase);
      phase_sin = std::sin(phase);
    }
    const double window = (i == 0 || i + 1 == n) ? 0.0 : 0.5 * (1.0 - phase_cos);
    x[i] = (float)(x[i] * window);

    const double next_cos = phase_cos * step_cos - phase_sin * step_sin;
    phase_sin = phase_sin * step_cos + phase_cos * step_sin;
    phase_cos = next_cos;
  }
}

void fft_radix2(float* re, float* im, std::size_t n) {
  std::array<float, kFftTwiddleChunk> twiddle_real{};
  std::array<float, kFftTwiddleChunk> twiddle_imaginary{};
  fft_radix2_impl(re, im, n, twiddle_real.data(), twiddle_imaginary.data());
}

void fft_radix2(float* re, float* im, std::size_t n, SpectrumWorkspace& workspace) {
  fft_radix2_impl(re, im, n, workspace.twiddle_real.data(), workspace.twiddle_imaginary.data());
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
    mean += (samples[i] - mean) / (double)(i + 1);
  }
  const double float_limit = (double)std::numeric_limits<float>::max();
  double scale = 0.0;
  for (std::size_t index = 0; index < n; index++) {
    const double centered = samples[index] - mean;
    if (!std::isfinite(centered) || std::fabs(centered) > float_limit) return out;
    if (std::fabs(centered) > scale) scale = std::fabs(centered);
  }
  if (scale == 0.0) return out;
  for (std::size_t index = 0; index < n; index++) {
    workspace.real[index] = (float)((samples[index] - mean) / scale);
    workspace.imaginary[index] = 0.0f;
  }

  hann_window(workspace.real.data(), n);
  fft_radix2(workspace.real.data(), workspace.imaginary.data(), n, workspace);

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
  out.peak_amplitude = (std::sqrt(peak_power) * 4.0 / (double)n) * scale;

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
  const double rotation_step = 2.0 * kPi * (rotation_hz / sample_rate_hz);
  const double bearing_step = 2.0 * kPi * (120.0 / sample_rate_hz);
  for (std::size_t index = 0; index < n; index++) {
    const double phase = rotation_step * (double)index;
    const double fundamental = std::sin(phase);
    const double harmonic = 0.15 * std::sin(2.0 * phase);
    const double bearing =
        bearing_amplitude * std::sin(bearing_step * (double)index);
    out[index] = fundamental + harmonic + bearing;
  }
}

std::size_t serialize_sample_json(char* output, std::size_t capacity, const char* event_id,
                                  const char* device_id, const char* metric, double value,
                                  const char* level, double temp_c) {
  JsonWriter writer(output, capacity);
  if (!writer.append("{\"event_id\":") || !writer.appendString(event_id) ||
      !writer.append(",\"device_id\":") || !writer.appendString(device_id) ||
      !writer.append(",\"metric\":") || !writer.appendString(metric) ||
      !writer.append(",\"value\":") || !writer.appendDouble(value, 2) ||
      !writer.append(",\"level\":") || !writer.appendString(level) ||
      !writer.append(",\"temp_c\":") || !writer.appendDouble(temp_c, 1) ||
      !writer.append("}")) {
    if (output != nullptr && capacity > 0) output[0] = '\0';
    return 0;
  }
  return writer.size();
}

std::size_t serialize_spectrum_json(char* output, std::size_t capacity, const char* event_id,
                                    const char* device_id, const Spectrum& spectrum,
                                    const char* fault) {
  JsonWriter writer(output, capacity);
  if (!writer.append("{\"event_id\":") || !writer.appendString(event_id) ||
      !writer.append(",\"device_id\":") || !writer.appendString(device_id) ||
      !writer.append(",\"metric\":\"spectrum\",\"dominant_hz\":") ||
      !writer.appendDouble(spectrum.dominant_hz, 1) ||
      !writer.append(",\"peak_amplitude\":") || !writer.appendDouble(spectrum.peak_amplitude, 3) ||
      !writer.append(",\"low_band\":") || !writer.appendDouble(spectrum.low_band, 3) ||
      !writer.append(",\"mid_band\":") || !writer.appendDouble(spectrum.mid_band, 3) ||
      !writer.append(",\"high_band\":") || !writer.appendDouble(spectrum.high_band, 3) ||
      !writer.append(",\"fault\":") || !writer.appendString(fault) || !writer.append("}")) {
    if (output != nullptr && capacity > 0) output[0] = '\0';
    return 0;
  }
  return writer.size();
}
