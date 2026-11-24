import 'dart:convert';

import 'package:http/http.dart' as http;

const String apiBase = String.fromEnvironment(
  'PULSE_API',
  defaultValue: 'http://10.0.2.2:8080',
);

Uri get apiBaseUri {
  return parseApiBase(apiBase);
}

Uri parseApiBase(String value, {bool isRelease = const bool.fromEnvironment('dart.vm.product')}) {
  final uri = Uri.tryParse(value);
  if (uri == null || !uri.hasAuthority || uri.host.isEmpty || uri.userInfo.isNotEmpty) {
    throw const FormatException('PULSE_API must be an absolute HTTP or HTTPS URL');
  }
  if (uri.scheme != 'http' && uri.scheme != 'https') {
    throw const FormatException('PULSE_API scheme must be HTTP or HTTPS');
  }
  if (isRelease && uri.scheme != 'https') {
    throw const FormatException('Release builds require an HTTPS PULSE_API URL');
  }
  return uri;
}

Uri buildApiUri(String baseUrl, String path, [Map<String, String>? query]) {
  final base = parseApiBase(baseUrl);
  final basePath = base.path.endsWith('/') ? base.path.substring(0, base.path.length - 1) : base.path;
  final requestPath = path.startsWith('/') ? path : '/$path';
  return base.replace(path: '$basePath$requestPath', queryParameters: query);
}

Uri apiUri(String path, [Map<String, String>? query]) => buildApiUri(apiBase, path, query);

class Sample {
  final String deviceId;
  final String metric;
  final double value;
  final String level;
  final String ts;

  Sample({
    required this.deviceId,
    required this.metric,
    required this.value,
    required this.level,
    required this.ts,
  });

  factory Sample.fromJson(Map<String, dynamic> json) {
    return Sample(
      deviceId: json['device_id'] as String? ?? 'unknown',
      metric: json['metric'] as String? ?? '',
      value: (json['value'] as num?)?.toDouble() ?? 0,
      level: json['level'] as String? ?? '',
      ts: json['ts'] as String? ?? '',
    );
  }
}

class AlertItem {
  final String id;
  final String deviceId;
  final String metric;
  final String level;
  final String message;
  final bool acked;

  AlertItem({
    required this.id,
    required this.deviceId,
    required this.metric,
    required this.level,
    required this.message,
    required this.acked,
  });

  factory AlertItem.fromJson(Map<String, dynamic> json) {
    return AlertItem(
      id: json['id'] as String? ?? '',
      deviceId: json['device_id'] as String? ?? 'unknown',
      metric: json['metric'] as String? ?? '',
      level: json['level'] as String? ?? '',
      message: json['message'] as String? ?? '',
      acked: json['acked'] as bool? ?? false,
    );
  }

  AlertItem copyWith({bool? acked}) {
    return AlertItem(
      id: id,
      deviceId: deviceId,
      metric: metric,
      level: level,
      message: message,
      acked: acked ?? this.acked,
    );
  }
}

class Diagnosis {
  final String eventId;
  final String deviceId;
  final double dominantHz;
  final double peakAmplitude;
  final double lowBand;
  final double midBand;
  final double highBand;
  final String fault;
  final String diagnosedAt;

  Diagnosis({
    required this.eventId,
    required this.deviceId,
    required this.dominantHz,
    required this.peakAmplitude,
    required this.lowBand,
    required this.midBand,
    required this.highBand,
    required this.fault,
    required this.diagnosedAt,
  });

  factory Diagnosis.fromJson(Map<String, dynamic> json) {
    return Diagnosis(
      eventId: json['event_id'] as String? ?? '',
      deviceId: json['device_id'] as String? ?? 'unknown',
      dominantHz: (json['dominant_hz'] as num?)?.toDouble() ?? 0,
      peakAmplitude: (json['peak_amplitude'] as num?)?.toDouble() ?? 0,
      lowBand: (json['low_band'] as num?)?.toDouble() ?? 0,
      midBand: (json['mid_band'] as num?)?.toDouble() ?? 0,
      highBand: (json['high_band'] as num?)?.toDouble() ?? 0,
      fault: json['fault'] as String? ?? 'unknown',
      diagnosedAt: json['diagnosed_at'] as String? ?? '',
    );
  }
}

class ApiClient {
  final http.Client _client;

  ApiClient([http.Client? client]) : _client = client ?? http.Client();

  void close() => _client.close();

  dynamic _decodeSuccess(http.Response response) {
    if (response.statusCode < 200 || response.statusCode >= 300) {
      var message = 'HTTP ${response.statusCode}';
      try {
        final body = jsonDecode(response.body);
        if (body is Map<String, dynamic> && body['error'] is String) {
          message = body['error'] as String;
        } else if (response.body.isNotEmpty) {
          message = response.body;
        }
      } on FormatException {
        if (response.body.isNotEmpty) message = response.body;
      }
      throw ApiException(response.statusCode, message);
    }
    return jsonDecode(response.body);
  }

  Future<List<Sample>> samples() async {
    final resp = await _client
        .get(apiUri('/api/v1/samples'))
        .timeout(const Duration(seconds: 5));
    final data = _decodeSuccess(resp) as List<dynamic>;
    return data
        .map((item) => Sample.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<List<AlertItem>> alerts({bool includeAcked = false}) async {
    final query = includeAcked ? {'include_acked': '1'} : null;
    final resp = await _client
        .get(apiUri('/api/v1/alerts', query))
        .timeout(const Duration(seconds: 5));
    final data = _decodeSuccess(resp) as List<dynamic>;
    return data
        .map((item) => AlertItem.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<List<Diagnosis>> diagnoses({String device = '', int limit = 50}) async {
    if (limit < 1 || limit > 100) {
      throw RangeError.range(limit, 1, 100, 'limit');
    }
    final query = <String, String>{'limit': '$limit'};
    if (device.isNotEmpty) query['device'] = device;
    final response = await _client
        .get(apiUri('/api/v1/diagnoses', query))
        .timeout(const Duration(seconds: 5));
    final data = _decodeSuccess(response) as List<dynamic>;
    return data
        .map((item) => Diagnosis.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<void> ack(String id) async {
    if (id.isEmpty) {
      throw const FormatException('alert id must not be empty');
    }
    final encodedId = Uri.encodeComponent(id);
    final response = await _client
        .post(apiUri('/api/v1/alerts/$encodedId/ack'))
        .timeout(const Duration(seconds: 5));
    _decodeSuccess(response);
  }
}

class ApiException implements Exception {
  final int statusCode;
  final String message;

  const ApiException(this.statusCode, this.message);

  @override
  String toString() => 'ApiException($statusCode): $message';
}
