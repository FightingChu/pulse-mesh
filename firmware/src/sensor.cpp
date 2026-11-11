#include "sensor.h"
#include <cmath>
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
