import 'dart:convert';

import 'package:http/http.dart' as http;

const String apiBase = String.fromEnvironment(
  'PULSE_API',
  defaultValue: 'http://10.0.2.2:8080',
);

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
        .get(Uri.parse('$apiBase/api/v1/samples'))
        .timeout(const Duration(seconds: 5));
    final data = _decodeSuccess(resp) as List<dynamic>;
    return data
        .map((item) => Sample.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<List<AlertItem>> alerts({bool includeAcked = false}) async {
    final query = includeAcked ? '?include_acked=1' : '';
    final resp = await _client
        .get(Uri.parse('$apiBase/api/v1/alerts$query'))
        .timeout(const Duration(seconds: 5));
    final data = _decodeSuccess(resp) as List<dynamic>;
    return data
        .map((item) => AlertItem.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<void> ack(String id) async {
    final response = await _client
        .post(Uri.parse('$apiBase/api/v1/alerts/$id/ack'))
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
