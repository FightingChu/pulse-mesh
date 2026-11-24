import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pulse_mesh_mobile/api.dart';

void main() {
  test('API base validation rejects user info and unsupported schemes', () {
    expect(() => parseApiBase('https://user:pass@example.com'), throwsFormatException);
    expect(() => parseApiBase('ftp://example.com'), throwsFormatException);
    expect(parseApiBase('http://10.0.2.2:8080', isRelease: true), throwsFormatException);
    expect(parseApiBase('https://example.com/base').scheme, 'https');
  });

  test('API URI builder preserves configured base path', () {
    final uri = buildApiUri('https://example.com/pulse/', '/api/v1/samples');
    expect(uri.toString(), 'https://example.com/pulse/api/v1/samples');
  });

  test('samples decodes the API payload', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.url.path, '/api/v1/samples');
      return http.Response(
        jsonEncode([
          {
            'device_id': 'motor-01',
            'metric': 'vibration_rms',
            'value': 1.25,
            'level': 'normal',
            'ts': '2026-11-12T00:00:00Z',
          }
        ]),
        200,
      );
    }));
    addTearDown(client.close);

    final samples = await client.samples();
    expect(samples, hasLength(1));
    expect(samples.single.deviceId, 'motor-01');
    expect(samples.single.value, 1.25);
  });

  test('alerts includes acknowledged filter', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.url.queryParameters['include_acked'], '1');
      return http.Response('[]', 200);
    }));
    addTearDown(client.close);

    expect(await client.alerts(includeAcked: true), isEmpty);
  });

  test('diagnoses requests bounded history and decodes spectral result', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.url.path, '/api/v1/diagnoses');
      expect(request.url.queryParameters['limit'], '50');
      return http.Response(
        jsonEncode([
          {
            'event_id': 'boot-1-spectrum-0',
            'device_id': 'motor-01',
            'dominant_hz': 120.12,
            'peak_amplitude': 0.6,
            'low_band': 0.0,
            'mid_band': 0.1,
            'high_band': 0.9,
            'fault': 'bearing_wear',
            'diagnosed_at': '2026-11-12T00:00:00Z',
          }
        ]),
        200,
      );
    }));
    addTearDown(client.close);

    final results = await client.diagnoses();
    expect(results, hasLength(1));
    expect(results.single.fault, 'bearing_wear');
    expect(results.single.dominantHz, 120.12);
  });

  test('diagnoses rejects an out-of-range limit before request', () async {
    var called = false;
    final client = ApiClient(MockClient((request) async {
      called = true;
      return http.Response('[]', 200);
    }));
    addTearDown(client.close);

    await expectLater(client.diagnoses(limit: 101), throwsRangeError);
    expect(called, isFalse);
  });

  test('ack throws API error on non-success response', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.method, 'POST');
      expect(request.url.path, '/api/v1/alerts/alert-4/ack');
      return http.Response('{"error":"alert not found"}', 404);
    }));
    addTearDown(client.close);

    await expectLater(
      client.ack('alert-4'),
      throwsA(
        isA<ApiException>()
            .having((error) => error.statusCode, 'statusCode', 404)
            .having((error) => error.message, 'message', 'alert not found'),
      ),
    );
  });

  test('ack encodes alert ID as a single path segment', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.url.pathSegments, ['api', 'v1', 'alerts', 'alert/part', 'ack']);
      return http.Response('{"status":"acked"}', 200);
    }));
    addTearDown(client.close);

    await client.ack('alert/part');
  });

  test('non-JSON API error remains a readable exception', () async {
    final client = ApiClient(MockClient((request) async {
      return http.Response('<html>gateway failure</html>', 502);
    }));
    addTearDown(client.close);

    await expectLater(
      client.alerts(),
      throwsA(
        isA<ApiException>()
            .having((error) => error.statusCode, 'statusCode', 502)
            .having((error) => error.message, 'message', '<html>gateway failure</html>'),
      ),
    );
  });

  test('ack rejects an empty alert ID before making a request', () async {
    var called = false;
    final client = ApiClient(MockClient((request) async {
      called = true;
      return http.Response('', 200);
    }));
    addTearDown(client.close);

    await expectLater(client.ack(''), throwsFormatException);
    expect(called, isFalse);
  });
}
