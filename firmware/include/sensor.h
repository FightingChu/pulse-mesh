#pragma once
#include <array>
#include <cstddef>

constexpr std::size_t kMaxSpectrumSamples = 1024;
constexpr std::size_t kFftTwiddleChunk = 32;

struct Reading { double vibration_rms; double temp_c; double current_a; };

struct Spectrum {
  double dominant_hz;
  double peak_amplitude;
  double low_band;
  double mid_band;
  double high_band;
};

struct SpectrumWorkspace {
  std::array<float, kMaxSpectrumSamples> real{};
  std::array<float, kMaxSpectrumSamples> imaginary{};
  std::array<float, kFftTwiddleChunk> twiddle_real{};
  std::array<float, kFftTwiddleChunk> twiddle_imaginary{};
};

double compute_rms(const double* v, std::size_t n);
int classify(double rms);
Reading sample_once(int tick);

void hann_window(float* x, std::size_t n);
void fft_radix2(float* re, float* im, std::size_t n);
void fft_radix2(float* re, float* im, std::size_t n, SpectrumWorkspace& workspace);
Spectrum analyze_spectrum(const double* samples, std::size_t n, double sample_rate_hz);
Spectrum analyze_spectrum(const double* samples, std::size_t n, double sample_rate_hz,
                          SpectrumWorkspace& workspace);
const char* classify_fault(const Spectrum& s, double rotation_hz);
void synthesize_waveform(double* out, std::size_t n, double sample_rate_hz,
                         double rotation_hz, double bearing_amplitude);
std::size_t serialize_sample_json(char* output, std::size_t capacity, const char* event_id,
                                  const char* device_id, const char* metric, double value,
                                  const char* level, double temp_c);
std::size_t serialize_spectrum_json(char* output, std::size_t capacity, const char* event_id,
                                    const char* device_id, const Spectrum& spectrum,
                                    const char* fault);
