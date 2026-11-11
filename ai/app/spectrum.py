import cmath
import math


def is_power_of_two(n):
    return n >= 2 and (n & (n - 1)) == 0


def hann(n):
    if n < 2:
        return [1.0] * n
    return [0.5 * (1.0 - math.cos(2.0 * math.pi * i / (n - 1))) for i in range(n)]


def fft(values):
    n = len(values)
    if not is_power_of_two(n):
        raise ValueError("fft length must be a power of two")
    a = [complex(v) for v in values]

    j = 0
    for i in range(1, n):
        bit = n >> 1
        while j & bit:
            j ^= bit
            bit >>= 1
        j ^= bit
        if i < j:
            a[i], a[j] = a[j], a[i]

    length = 2
    while length <= n:
        wlen = cmath.exp(-2j * math.pi / length)
        for start in range(0, n, length):
            w = 1 + 0j
            half = length // 2
            for k in range(half):
                u = a[start + k]
                v = a[start + k + half] * w
                a[start + k] = u + v
                a[start + k + half] = u - v
                w *= wlen
        length <<= 1
    return a


def analyze(values, sample_rate_hz):
    n = len(values)
    if not is_power_of_two(n) or n < 8:
        raise ValueError("values length must be a power of two and at least 8")
    if sample_rate_hz <= 0:
        raise ValueError("sample_rate_hz must be positive")

    mean = sum(values) / n
    window = hann(n)
    prepared = [(values[i] - mean) * window[i] for i in range(n)]
    spectrum = fft(prepared)

    half = n // 2
    power = [0.0] * (half + 1)
    total = 0.0
    peak_bin = 1
    for k in range(1, half + 1):
        power[k] = abs(spectrum[k]) ** 2
        total += power[k]
        if power[k] > power[peak_bin]:
            peak_bin = k
    if total <= 0.0:
        return {
            "dominant_hz": 0.0,
            "peak_amplitude": 0.0,
            "low_band": 0.0,
            "mid_band": 0.0,
            "high_band": 0.0,
        }

    bin_hz = sample_rate_hz / n
    bands = {"low_band": 0.0, "mid_band": 0.0, "high_band": 0.0}
    for k in range(1, half + 1):
        hz = k * bin_hz
        if hz < 10.0:
            bands["low_band"] += power[k]
        elif hz < 100.0:
            bands["mid_band"] += power[k]
        elif hz <= 500.0:
            bands["high_band"] += power[k]

    return {
        "dominant_hz": round(peak_bin * bin_hz, 2),
        "peak_amplitude": round(abs(spectrum[peak_bin]) * 4.0 / n, 4),
        "low_band": round(bands["low_band"] / total, 4),
        "mid_band": round(bands["mid_band"] / total, 4),
        "high_band": round(bands["high_band"] / total, 4),
    }


def classify_fault(features, rotation_hz):
    if features["high_band"] > 0.25:
        return "bearing_wear"
    if rotation_hz > 0:
        ratio = features["dominant_hz"] / rotation_hz
        if 1.8 <= ratio <= 2.2:
            return "misalignment"
        if 0.8 <= ratio <= 1.2:
            return "imbalance"
    if features["low_band"] > 0.5:
        return "looseness"
    return "normal"


def diagnose(values, sample_rate_hz, rotation_hz):
    features = analyze(values, sample_rate_hz)
    features["fault"] = classify_fault(features, rotation_hz)
    return features
