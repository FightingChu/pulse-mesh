#pragma once
#include <cstddef>
struct Reading { double vibration_rms; double temp_c; double current_a; };
double compute_rms(const double* v, std::size_t n);
int classify(double rms);
Reading sample_once(int tick);
