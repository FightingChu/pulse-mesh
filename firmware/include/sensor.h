#pragma once
#include <cstddef>

struct Reading { double vibration_rms; double temp_c; double current_a; };

struct Spectrum {
  double dominant_hz;
  double peak_amplitude;
  double low_band;
  double mid_band;
  double high_band;
};

double compute_rms(const double* v, std::size_t n);
int classify(double rms);
Reading sample_once(int tick);

void hann_window(double* x, std::size_t n);
void fft_radix2(double* re, double* im, std::size_t n);
Spectrum analyze_spectrum(const double* samples, std::size_t n, double sample_rate_hz);
const char* classify_fault(const Spectrum& s, double rotation_hz);
void synthesize_waveform(double* out, std::size_t n, double sample_rate_hz,
                         double rotation_hz, double bearing_amplitude);
